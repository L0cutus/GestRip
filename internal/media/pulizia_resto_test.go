package media

import "testing"

// Le parti di MediaCleanup (RIP-S-57) che non stanno nella slice 041. Finché questi test sono
// saltati, la voce resta parziale; ognuna va nel giro (Pulizia.giro, al suo punto) con la sua slice.

// inventario: RIP-S-57
// cleanup_stale_exports/0: a ogni giro, prima del resto, le esportazioni vecchie
// (InstanceExport.cleanup_stale/1) nella cartella di lavoro delle esportazioni.
func TestPuliziaEsportazioniVecchie(t *testing.T) {
	t.Skip("esportazioni vecchie: con l'esportazione dell'istanza")
}

// inventario: RIP-S-57
// run_retention/3: una volta al giorno del negozio la conservazione delle foto
// (PhotoRetention.run/0), riprovata al giro dopo se fallisce; è legata al registro dei backup
// verificati.
func TestPuliziaConservazioneFotoUnaVoltaAlGiorno(t *testing.T) {
	t.Skip("conservazione delle foto: T4.11")
}

// inventario: RIP-S-57
// run_training_expiry/3: una volta al giorno del negozio la scadenza dei dettati di addestramento
// oltre dodici mesi (Assist.Training.expire/0), riprovata al giro dopo; nel registro solo il tipo
// dell'errore, mai il messaggio.
func TestPuliziaScadenzaDeiDettati(t *testing.T) {
	t.Skip("scadenza dei dettati: T4.10")
}
