package sintetiche

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/jpeg"
	"strconv"
)

// UltraHDR è una foto JPEG Ultra HDR come la scrive la galleria di un telefono dopo una modifica
// (slice 222 dello standalone, la foto dell'Oppo Find X9 Ultra del 03/11/2026, mai nel repo): la
// foto principale con EXIF (produttore e, se non è 0, orientamento), un APP1 XMP con lo spazio
// `hdrgm` e il contenitore GContainer, il profilo ICC e un APP2 «MPF» (CIPA DC-007, big endian)
// con due voci; in coda, come seconda JPEG, la gain map in scala di grigi con il suo XMP `hdrgm`.
// Le voci MPF dicono la dimensione vera delle due immagini e la posizione della gain map
// (DataOffset dall'intestazione TIFF dell'MPF), così Pillow 9.4 la apre come MPO di 2 fotogrammi.
func UltraHDR(principale, gainMap image.Image, orientamento int) []byte {
	mappa := codifica(gainMap, 85)
	xmpMappa := `<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		`<rdf:Description xmlns:hdrgm="http://ns.adobe.com/hdr-gain-map/1.0/" hdrgm:Version="1.0" ` +
		`hdrgm:GainMapMin="0" hdrgm:GainMapMax="2" hdrgm:Gamma="1" hdrgm:OffsetSDR="0.015625" ` +
		`hdrgm:OffsetHDR="0.015625" hdrgm:HDRCapacityMin="0" hdrgm:HDRCapacityMax="2" ` +
		`hdrgm:BaseRenditionIsHDR="False"/></rdf:RDF></x:xmpmeta>`
	mappa = SegmentoJPEG(mappa, 0xE1, append([]byte("http://ns.adobe.com/xap/1.0/\x00"), xmpMappa...))

	xmp := `<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		`<rdf:Description xmlns:hdrgm="http://ns.adobe.com/hdr-gain-map/1.0/" ` +
		`xmlns:Container="http://ns.google.com/photos/1.0/container/" ` +
		`xmlns:Item="http://ns.google.com/photos/1.0/container/item/" hdrgm:Version="1.0">` +
		`<Container:Directory><rdf:Seq>` +
		`<rdf:li rdf:parseType="Resource"><Container:Item Item:Semantic="Primary" Item:Mime="image/jpeg"/></rdf:li>` +
		`<rdf:li rdf:parseType="Resource"><Container:Item Item:Semantic="GainMap" Item:Mime="image/jpeg" ` +
		`Item:Length="` + strconv.Itoa(len(mappa)) + `"/></rdf:li>` +
		`</rdf:Seq></Container:Directory></rdf:Description></rdf:RDF></x:xmpmeta>`

	dati := codifica(principale, 92)
	testa := segmento(0xE1, append([]byte("Exif\x00\x00"), TIFF(orientamento)...))
	testa = append(testa, segmento(0xE1, append([]byte("http://ns.adobe.com/xap/1.0/\x00"), xmp...))...)
	testa = append(testa, segmento(0xE2, append([]byte("ICC_PROFILE\x00\x01\x01"), "synthetic icc profile"...))...)
	// L'intestazione TIFF dell'MPF sta dopo SOI, i segmenti di prima, marcatore e lunghezza
	// (4 byte) e «MPF\0» (4 byte): DataOffset si conta da lì.
	inizioMPF := 2 + len(testa) + 4 + 4
	lunghezzaMPF := 4 + len(indiceMPF(0, 0, 0))
	totale := len(dati) + len(testa) + 4 + lunghezzaMPF
	testa = append(testa, segmento(0xE2, append([]byte("MPF\x00"), indiceMPF(totale, len(mappa), totale-inizioMPF)...))...)
	out := append(append(append([]byte{}, dati[:2]...), testa...), dati[2:]...)
	if len(out) != totale {
		panic("sintetiche: lunghezza della Ultra HDR")
	}
	return append(out, mappa...)
}

// indiceMPF è l'IFD dell'MPF: versione, due immagini, le due voci (la principale 0x030000,
// «Baseline MP Primary Image», a DataOffset 0; la gain map 0, a offset) e nessun IFD seguente.
func indiceMPF(primaria, mappa, offset int) []byte {
	p := []byte("MM\x00\x2A\x00\x00\x00\x08")
	p = binary.BigEndian.AppendUint16(p, 3)
	p = binary.BigEndian.AppendUint16(p, 0xB000)
	p = binary.BigEndian.AppendUint16(p, 7)
	p = binary.BigEndian.AppendUint32(p, 4)
	p = append(p, "0100"...)
	p = binary.BigEndian.AppendUint16(p, 0xB001)
	p = binary.BigEndian.AppendUint16(p, 4)
	p = binary.BigEndian.AppendUint32(p, 1)
	p = binary.BigEndian.AppendUint32(p, 2)
	p = binary.BigEndian.AppendUint16(p, 0xB002)
	p = binary.BigEndian.AppendUint16(p, 7)
	p = binary.BigEndian.AppendUint32(p, 32)
	p = binary.BigEndian.AppendUint32(p, uint32(8+2+3*12+4))
	p = binary.BigEndian.AppendUint32(p, 0)
	for _, v := range [][2]int{{0x030000, primaria}, {0, mappa}} {
		p = binary.BigEndian.AppendUint32(p, uint32(v[0]))
		p = binary.BigEndian.AppendUint32(p, uint32(v[1]))
		if v[0] == 0 {
			p = binary.BigEndian.AppendUint32(p, uint32(offset))
		} else {
			p = binary.BigEndian.AppendUint32(p, 0)
		}
		p = binary.BigEndian.AppendUint32(p, 0)
	}
	return p
}

func codifica(m image.Image, qualita int) []byte {
	var b bytes.Buffer
	if err := jpeg.Encode(&b, m, &jpeg.Options{Quality: qualita}); err != nil {
		panic(err)
	}
	return b.Bytes()
}
