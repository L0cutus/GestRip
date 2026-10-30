# Domande aperte per Stefano

Raccolte durante il lavoro per tappa (`prompt-via-area.md`). Ogni domanda ha la raccomandazione
della sessione; il lavoro che non ne dipende va avanti. Quando Stefano risponde, la domanda passa
nell'inventario o nel piano e qui si toglie.

## Aperte

1. **Nome del servizio systemd di `gestrip serve`** (slice 013b, 30/10/2026). Il messaggio della
   pagina utenti senza `indirizzo_interno` dice di riavviare con `sudo systemctl restart gestrip`:
   serviva un comando esatto e l'installer (T5) non esiste ancora. **Raccomandazione:** tenere
   `gestrip.service`. Se l'installer sceglie un altro nome, cambia una costante
   (`apparecchio.ComandoRiavvio`) e il test lo segue.
