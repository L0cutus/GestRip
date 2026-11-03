package lettura

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Il lettore regge un PDF ostile (review della 042): ogni caso finisce con un errore, in poco tempo e
// con poca memoria, mai con un crollo del processo.

// zlibDi comprime b.
func zlibDi(t *testing.T, b []byte) []byte {
	t.Helper()
	var z bytes.Buffer
	w := zlib.NewWriter(&z)
	if _, err := w.Write(b); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return z.Bytes()
}

func streamFlate(t *testing.T, dati []byte) string {
	t.Helper()
	z := zlibDi(t, dati)
	return fmt.Sprintf("<< /Length %d /Filter /FlateDecode >>\nstream\n%s\nendstream", len(z), z)
}

// unaPagina è un PDF di una pagina con il contenuto e il font F1 (Helvetica) dati.
func unaPagina(contenuto string, altri ...string) []byte {
	oo := append([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 /MediaBox [0 0 100 100] >>",
		"<< /Type /Page /Parent 2 0 R /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		contenuto,
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}, altri...)
	return pdfAMano(oo...)
}

func rifiutato(t *testing.T, cosa string, b []byte) {
	t.Helper()
	inizio := time.Now()
	if _, err := Leggi(b); err == nil {
		t.Errorf("%s: letto senza errore", cosa)
	}
	if d := time.Since(inizio); d > 5*time.Second {
		t.Errorf("%s: %v per rifiutarlo", cosa, d)
	}
}

// Array e dizionari annidati oltre la profondità massima sono un errore, anche a milioni di livelli
// (prima: stack overflow fatale); entro il limite si leggono.
func TestAnnidamentoProfondo(t *testing.T) {
	annidato := func(n int) string { return strings.Repeat("[", n) + strings.Repeat("]", n) }
	if _, err := Leggi(unaPagina(streamDi(annidato(profonditaMassima) + " pop"))); err != nil {
		t.Errorf("entro il limite: %v", err)
	}
	rifiutato(t, "array nel contenuto", unaPagina(streamDi(annidato(profonditaMassima+1)+" pop")))
	rifiutato(t, "dizionari in un oggetto", unaPagina(streamDi("0 0 1 1 re f"),
		strings.Repeat("<< /A ", profonditaMassima+1)+strings.Repeat(">> ", profonditaMassima+1)))
	rifiutato(t, "milioni di [", unaPagina(streamFlate(t, bytes.Repeat([]byte("["), 20_000_000))))
}

// Un albero delle pagine con lo stesso nodo ripetuto è un errore (prima: 64 figli uguali per tre
// livelli erano 262 144 pagine, 16 s e 3,6 GB); così oltre maxPagine pagine diverse.
func TestAlberoDellePagineOstile(t *testing.T) {
	rifiutato(t, "pagina ripetuta", pdfAMano(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 3 0 R] /Count 2 /MediaBox [0 0 100 100] >>",
		"<< /Type /Page /Parent 2 0 R /Contents 4 0 R >>",
		streamDi("0 0 1 1 re f"),
	))
	figli := strings.TrimSpace(strings.Repeat("3 0 R ", 64))
	rifiutato(t, "nodi ripetuti su tre livelli", pdfAMano(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids ["+figli+"] /Count 262144 /MediaBox [0 0 100 100] >>",
		"<< /Type /Pages /Kids ["+strings.ReplaceAll(figli, "3 0 R", "4 0 R")+"] /Count 4096 >>",
		"<< /Type /Pages /Kids ["+strings.ReplaceAll(figli, "3 0 R", "5 0 R")+"] /Count 64 >>",
		"<< /Type /Page /Contents 6 0 R >>",
		streamDi("0 0 1 1 re f"),
	))
	tante := func(n int) []byte {
		oo := []string{"<< /Type /Catalog /Pages 2 0 R >>", ""}
		var kids []string
		for i := range n {
			kids = append(kids, fmt.Sprintf("%d 0 R", i+3))
			oo = append(oo, "<< /Type /Page /MediaBox [0 0 10 10] >>")
		}
		oo[1] = "<< /Type /Pages /Kids [" + strings.Join(kids, " ") + "] >>"
		return pdfAMano(oo...)
	}
	if d, err := Leggi(tante(maxPagine)); err != nil || len(d.Pagine) != maxPagine {
		t.Errorf("%d pagine: %v", maxPagine, err)
	}
	rifiutato(t, "troppe pagine", tante(maxPagine+1))
}

// Molte pagine che decomprimono lo stesso stream: i byte decompressi si contano per tutto il
// documento, oltre maxDecompressi è un errore.
func TestByteDecompressiNelDocumento(t *testing.T) {
	oo := []string{"<< /Type /Catalog /Pages 2 0 R >>", "", streamFlate(t, make([]byte, 1<<20))}
	var kids []string
	n := int(maxDecompressi>>20) + 1
	for i := range n {
		kids = append(kids, fmt.Sprintf("%d 0 R", i+4))
		oo = append(oo, "<< /Type /Page /MediaBox [0 0 10 10] /Contents 3 0 R >>")
	}
	oo[1] = "<< /Type /Pages /Kids [" + strings.Join(kids, " ") + "] >>"
	rifiutato(t, "stesso stream per molte pagine", pdfAMano(oo...))
}

// Uno stream che decomprime oltre maxStream è un errore, non un contenuto troncato.
func TestStreamTroppoGrande(t *testing.T) {
	if testing.Short() {
		t.Skip("comprime 64 MiB")
	}
	rifiutato(t, "stream grande", unaPagina(streamFlate(t, make([]byte, maxStream+1))))
	if _, err := Leggi(unaPagina(streamFlate(t, make([]byte, maxStream)))); err != nil {
		t.Errorf("stream al limite: %v", err)
	}
}

// Molte immagini in linea si saltano in tempo lineare (prima: una copia del resto del contenuto a
// ogni BI), e il testo dopo un'immagine si legge.
func TestImmaginiInLinea(t *testing.T) {
	d, err := Leggi(unaPagina(streamDi("BI /W 1 /H 1 /BPC 8 /CS /G ID \x00 EI\nBT /F1 10 Tf 1 1 Td (dopo) Tj ET")))
	if err != nil || len(d.Pagine[0].Righe) != 1 || d.Pagine[0].Righe[0].Testo != "dopo" {
		t.Fatalf("%v %+v", err, d)
	}
	molte := strings.Repeat("BI /W 1 /H 1 ID x EI\n", 200_000)
	inizio := time.Now()
	if _, err := Leggi(unaPagina(streamFlate(t, []byte(molte)))); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(inizio); d > 5*time.Second {
		t.Errorf("200 000 immagini in linea in %v", d)
	}
}

// Una CMap piccola con intervalli enormi: oltre maxVociCMap voci è un errore (prima: 842 byte con
// 1000 bfrange erano 510 MB).
func TestCMapEnorme(t *testing.T) {
	cmap := "begincmap 10 beginbfrange\n" + strings.Repeat("<0000> <FFFF> <0041>\n", 10) + "endbfrange endcmap"
	font := "<< /Type /Font /Subtype /Type0 /Encoding /Identity-H /ToUnicode 6 0 R >>"
	b := pdfAMano(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /MediaBox [0 0 100 100] >>",
		"<< /Type /Page /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		streamDi("BT /F1 10 Tf <00410042> Tj ET"),
		font, streamDi(cmap),
	)
	rifiutato(t, "CMap enorme usata", b)
}

// Casi del lettore che le mutazioni della review hanno lasciato senza test.
func TestLettoreCasiDiConfine(t *testing.T) {
	cmap := streamDi("begincmap 1 beginbfchar <0041> <0041> endbfchar endcmap")
	conFont := func(testo string) []byte {
		return pdfAMano(
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /MediaBox [0 0 100 100] >>",
			"<< /Type /Page /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
			streamDi("BT /F1 10 Tf <"+testo+"> Tj ET"),
			"<< /Type /Font /Subtype /Type0 /Encoding /Identity-H /ToUnicode 6 0 R >>", cmap,
		)
	}
	if d, err := Leggi(conFont("0041")); err != nil || d.Pagine[0].Righe[0].Testo != "A" {
		t.Errorf("glifo nel ToUnicode: %v", err)
	}
	rifiutato(t, "glifo senza ToUnicode", conFont("00410042"))
	rifiutato(t, "DecodeParms", unaPagina("<< /Length 12 /DecodeParms << /Predictor 12 >> >>\nstream\n0 0 1 1 re f\nendstream"))
	buono := unaPagina(streamDi("0 0 1 1 re f"))
	rifiutato(t, "numero sbagliato all'offset", bytes.Replace(buono, []byte("\n4 0 obj"), []byte("\n7 0 obj"), 1))
	// Una voce «f» della xref: l'oggetto non si legge, anche se all'offset c'è spazzatura.
	font := "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"
	libera := bytes.Replace(buono, []byte(font), []byte("<< spazzatura ("+strings.Repeat(" ", len(font)-17)+">>"), 1)
	i := bytes.LastIndex(libera, []byte(" 00000 n \ntrailer"))
	libera = append(append(append([]byte{}, libera[:i]...), []byte(" 00000 f \ntrailer")...), libera[i+len(" 00000 n \ntrailer"):]...)
	if _, err := Leggi(libera); err != nil {
		t.Errorf("voce libera della xref: %v", err)
	}
	// /Prev che punta alla stessa xref: errore, non un ciclo infinito.
	x := bytes.LastIndex(buono, []byte("\nxref\n")) + 1 // non quello dentro «startxref»
	circolare := bytes.Replace(buono, []byte("/Root 1 0 R >>"), []byte(fmt.Sprintf("/Root 1 0 R /Prev %d >>", x)), 1)
	fatto := make(chan struct{})
	go func() { rifiutato(t, "/Prev circolare", circolare); close(fatto) }()
	select {
	case <-fatto:
	case <-time.After(10 * time.Second):
		t.Fatal("/Prev circolare: ciclo infinito")
	}
	// Albero delle pagine più profondo del limite.
	oo := []string{"<< /Type /Catalog /Pages 2 0 R >>"}
	for i := range profonditaPagine + 1 {
		oo = append(oo, fmt.Sprintf("<< /Type /Pages /Kids [%d 0 R] >>", i+3))
	}
	oo = append(oo, "<< /Type /Page /MediaBox [0 0 10 10] >>")
	rifiutato(t, "pagine troppo profonde", pdfAMano(oo...))
	// Form XObject annidati oltre il limite: ogni form disegna il successivo.
	ff := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /MediaBox [0 0 100 100] >>",
		"<< /Type /Page /Resources 4 0 R /Contents 5 0 R >>",
		"<< /XObject << /X 6 0 R >> >>",
		streamDi("/X Do"),
	}
	for i := range profonditaForme + 2 {
		ff = append(ff, fmt.Sprintf("<< /Type /XObject /Subtype /Form /Resources << /XObject << /X %d 0 R >> >> /Length 5 >>\nstream\n/X Do\nendstream", i+7))
	}
	ff = append(ff, streamDi("0 0 1 1 re f"))
	rifiutato(t, "forme troppo annidate", pdfAMano(ff...))
	poche := append(append([]string{}, ff[:6]...), streamDi("0 0 1 1 re f"))
	poche[5] = "<< /Type /XObject /Subtype /Form /Length 12 >>\nstream\n0 0 1 1 re f\nendstream"
	if d, err := Leggi(pdfAMano(poche[:6]...)); err != nil || len(d.Pagine[0].Rettangoli) != 1 {
		t.Errorf("una forma: %v", err)
	}
}

// Dentro un solo blocco BT…ET una riga nuova comincia quando la linea di base cambia (Td verticale)
// e non quando il testo va solo avanti sulla stessa linea (Chromium mette ogni glifo con il suo Td).
// gopdf e Chromium aprono comunque un BT per riga: il caso serve ad altri generatori, e resta perché
// costa poco ed è provato qui.
func TestRigheSullaLineaDiBase(t *testing.T) {
	d, err := Leggi(unaPagina(streamDi("BT /F1 10 Tf 1 0 0 1 5 80 Tm (a) Tj 6 0 Td (b) Tj 0 -12 Td (c) Tj ET")))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range d.Pagine[0].Righe {
		got = append(got, r.Testo)
	}
	if strings.Join(got, "|") != "ab|c" {
		t.Errorf("righe %q", got)
	}
}
