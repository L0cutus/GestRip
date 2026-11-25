package db

import (
	"testing"
	"time"
)

// Le letture del registro per la pagina Backup e il banner (stato_backup.go, slice 126). Dati inventati.

// Voce OPS-28, in parte: niente segno «inventario:» (la voce chiude con 127 e 128).
// UltimeVerificheBackup dà le righe dalla più recente (per id), con ogni esito e destinazione, fino al limite.
// IstantiRegistroBackup: il primo backup buono sulla chiavetta (non le fallite né le cartelle) e l'istante più
// avanti di tutto il registro, anche di una verifica (non solo della cattura) e di una riga fallita.
func TestLettureStatoBackup(t *testing.T) {
	d := registroProva(t)
	if v, err := UltimeVerificheBackup(ctx, d.Lettura, 10); err != nil || len(v) != 0 {
		t.Errorf("vuoto: %v %v", v, err)
	}
	if p, a, err := IstantiRegistroBackup(ctx, d.Lettura); err != nil || !p.IsZero() || !a.IsZero() {
		t.Errorf("vuoto: %v %v %v", p, a, err)
	}
	f, sha, n := archivioProva(t, "archivio inventato")
	cartella := verificaProva(f, sha, n, primaProva.Add(-48*time.Hour))
	cartella.Destinazione = DestinazioneCartella
	registra(t, d, cartella)
	fallita := verificaProva(f, sha, n, primaProva.Add(-24*time.Hour))
	fallita.Riuscita, fallita.Errore = false, "motivo inventato"
	registra(t, d, fallita)
	buona := verificaProva(f, sha, n, primaProva)
	buona.MediaRovinati = []string{sha}
	registra(t, d, buona)
	seconda := verificaProva(f, sha, n, dopoProva)
	seconda.VerificatoIl = dopoProva.Add(3 * time.Hour)
	registra(t, d, seconda)
	v, err := UltimeVerificheBackup(ctx, d.Lettura, 3)
	if err != nil || len(v) != 3 {
		t.Fatalf("righe: %v %v", v, err)
	}
	if !v[0].CatturatoIl.Equal(dopoProva) || !v[0].VerificatoIl.Equal(dopoProva.Add(3*time.Hour)) || !v[0].Riuscita ||
		v[1].MediaRovinati != 1 || v[1].Archivio != f || v[1].Byte != n || v[1].Destinazione != DestinazioneChiavetta ||
		v[2].Riuscita || v[2].Errore != "motivo inventato" || v[0].Errore != "" {
		t.Errorf("righe: %+v", v)
	}
	p, a, err := IstantiRegistroBackup(ctx, d.Lettura)
	if err != nil || !p.Equal(primaProva) || !a.Equal(dopoProva.Add(3*time.Hour)) {
		t.Errorf("istanti: %v %v %v", p, a, err)
	}
	// Una riga fallita con la cattura più avanti di tutte.
	futura := verificaProva(f, sha, n, dopoProva.Add(10*time.Hour))
	futura.Riuscita, futura.Errore = false, "motivo inventato"
	registra(t, d, futura)
	if p, a, err := IstantiRegistroBackup(ctx, d.Lettura); err != nil || !p.Equal(primaProva) || !a.Equal(dopoProva.Add(10*time.Hour+time.Minute)) {
		t.Errorf("con la fallita: %v %v %v", p, a, err)
	}
	// Un database che non risponde è un errore (un istante illeggibile non entra: lo ferma il CHECK della tabella).
	_ = d.Chiudi()
	if _, _, err := IstantiRegistroBackup(ctx, d.Lettura); err == nil {
		t.Error("database chiuso: nessun errore")
	}
	if _, err := UltimeVerificheBackup(ctx, d.Lettura, 3); err == nil {
		t.Error("database chiuso: nessun errore")
	}
}
