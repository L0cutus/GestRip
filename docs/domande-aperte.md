# Domande aperte per Stefano

Raccolte durante il lavoro per tappa (`prompt-via-area.md`). Ogni domanda ha la raccomandazione
della sessione; il lavoro che non ne dipende va avanti. Quando Stefano risponde, la domanda passa
nell'inventario o nel piano e qui si toglie.

## Aperte

1. **Primo titolare (ACC-16, slice 020): dettagli da confermare.**
   - Il link di `gestrip primo-titolare` vale 60 minuti: il reset ne ha 30 e l'invito 15. Serve il
     tempo di passare dal terminale a un browser nella tailnet, e il link vale solo con `utenti`
     vuota e solo sulla faccia interna.
   - Una riga d'audit in più quando si crea il link: `administrator.bootstrap_requested`, attore
     `console`. È sul modello di `password_reset.requested` di `reset-link`.
   - `/` e `/login` su un database vuoto non dicono niente di nuovo: il login non deve rivelare se
     il database è vuoto, e il comando lo indica l'installer.
   - Nessun limite ai tentativi su `/configura`, come per `/reset/` e `/invito/` sulla faccia
     interna: il codice è di 256 bit.

   **Raccomandazione:** tenere le quattro scelte così.

## Risposte già registrate

Le domande 1–15 (slice 013b–018) sono state approvate da Stefano il 30/10/2026 e
registrate: deviazioni D-34…D-41, note nell'inventario (ACC-16, ADM-17, ADM-20, ADM-22, OPS-12) e
nel piano (T2, T4, T5, §7.3).
