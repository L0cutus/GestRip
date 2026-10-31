# Prova dell'import sulla copia dei dati veri: esito anonimo

- Verdetto: **riconciliato**
- Commit del Go: `7e28403`
- Data: 2026-10-31
- Modalità: autorizzato

## Tabelle

| tabella | differenze | righe scartate |
| --- | --- | --- |
| users | no | no |
| user_invitations | no | no |
| password_resets | no | no |
| settings | no | no |
| audit_events | no | no |
| shop_profiles | no | no |
| branding_assets | no | no |
| customers | no | no |
| bag_sequences | no | no |
| oreficerie | no | no |
| artisans | no | no |
| handovers | no | no |
| ready_lots | no | no |
| repairs | no | no |
| repair_status_versions | no | no |
| repair_photos | no | no |
| repair_messages | no | no |
| artisan_reminders | no | no |
| outgoing_messages | no | no |
| ready_notice_events | no | no |
| quote_decision_events | no | no |
| delivery_signature_events | no | no |
| telegram_subscriptions | no | no |
| telegram_link_tokens | no | no |
| dettati_addestramento | no | no |
| dettati_esclusioni | no | no |
| courtesy_document_sequences | no | no |
| sales | no | no |
| sale_revisions | no | no |
| sale_photos | no | no |
| sale_revision_photos | no | no |
| sale_revision_seals | no | no |
| sale_revision_revocations | no | no |
| emergency_events | no | no |
| emergency_entries | no | no |
| job_runs | no | no |
| media_deletion_jobs | no | no |
| historical_documents | no | no |
| legacy_import_mappings | no | no |
| legacy_import_captures | no | no |

## Avvisi

| codice | tabella | colonna | voci |
| --- | --- | --- | --- |
| tipo/impronta_testo | historical_documents | sha256 | 1 |
| tipo/impronta_testo | sale_photos | sha256 | 1 |
| tipo/impronta_testo | sale_revision_photos | sha256 | 1 |

## Differenze

Nessuna voce.

## Totali in centesimi

| tabella | colonna | tornano |
| --- | --- | --- |
| repairs | estimate_cents | sì |
| repairs | final_price_cents | sì |
| sales | price_cents | sì |
| sale_revisions | price_cents | sì |
| emergency_entries | gross_cents | sì |
| emergency_entries | taxable_cents | sì |
| emergency_entries | vat_cents | sì |

## Media

- Copiati: sì; file in più nella cartella: no; miniature contate: sì

| tabella | colonna | tutti copiati | mancanti | impronte diverse | altre differenze | già cancellati |
| --- | --- | --- | --- | --- | --- | --- |
| branding_assets | storage_key | sì | no | no | no | no |
| repairs | signature_storage_key | sì | no | no | no | no |
| repair_photos | storage_key | sì | no | no | no | no |
| delivery_signature_events | storage_key | sì | no | no | no | no |
| sale_photos | storage_key | sì | no | no | no | no |
| sale_revision_photos | storage_key | sì | no | no | no | no |
| historical_documents | storage_key | sì | no | no | no | no |
| media_deletion_jobs | storage_key | sì | no | no | no | no |

## Note

- Sorgente: l'ultimo backup verificato dello standalone, della notte del 31/10/2026, con l'immagine
  `fab-219`. La procedura è in `docs/prova-dati-veri.md`.
- Inventario dell'archivio: tutti i file uguali. Prova a secco sul banco verde prima della prova
  vera.
- `schema_migrations` dello standalone non si importa, per progetto, ed è l'unica tabella con righe
  scartate.
- Nessun istante senza zona nei dati veri: la regola della D-42 resta per le righe che lo
  standalone può ancora scrivere così.
- Gli avvisi `tipo/impronta_testo` sono quelli previsti dalla D-42.
- Nessuna domanda nuova per Stefano: nessuna differenza bloccante, nessun caso nuovo da
  trasformare in un dato inventato.
- Qui non ci sono numeri di righe, somme, id e impronte. Per costruzione il report non contiene
  valori dei dati; il report completo è stato cancellato con il tmpfs.
