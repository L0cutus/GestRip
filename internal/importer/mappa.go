package importer

// Costruttori corti, così ogni riga della mappa sta su una riga.
func u(s, g string) Colonna    { return Colonna{Standalone: s, Go: g, Traduzione: Uguale} }
func ist(s, g string) Colonna  { return Colonna{Standalone: s, Go: g, Traduzione: Istante} }
func data(s, g string) Colonna { return Colonna{Standalone: s, Go: g, Traduzione: Data} }
func bool01(s, g string) Colonna {
	return Colonna{Standalone: s, Go: g, Traduzione: Booleano}
}
func js(s, g string) Colonna { return Colonna{Standalone: s, Go: g, Traduzione: JSON} }

func (c Colonna) nota(n string) Colonna { c.Nota = n; return c }

var (
	id         = u("id", "id")
	creato     = ist("inserted_at", "creato_il")
	aggiornato = ist("updated_at", "aggiornato_il")
)

// Mappa è la mappa di tutte le tabelle dello standalone a go-baseline (6eb93ed), nell'ordine in
// cui l'import le scrive (i genitori prima dei figli).
//
// Regole comuni a tutte le righe:
//   - id: gli stessi numeri dello standalone (l'audit nomina le righe per numero); l'import porta
//     sqlite_sequence di ogni tabella a max(seq dello standalone, max id);
//   - soldi in centesimi interi, come lo standalone (e accanto il testo libero dove lo
//     standalone lo tiene: «vince il testo», OPS-03);
//   - gli stati e gli elenchi tengono le parole dello standalone (in inglese o in italiano come
//     sono), tranne il ruolo degli utenti, che il Go ha già tradotto;
//   - istanti in db.FormatoIstante: lo standalone li scrive con sei decimali e la Z (Ecto e il
//     trigger di Telegram), tranne la migrazione 20260918100000, che con strftime('%fZ') ne ha
//     scritti con tre (per esempio repairs.retention_started_at): l'import li riscrive;
//   - hash e sha256 vanno in colonne BLOB di tabelle STRICT, che rifiutano un TEXT. Lo standalone
//     non è STRICT e il suo trigger sul codice Flask fa CAST(… AS BLOB), quindi un hash salvato
//     come testo è possibile: l'import controlla typeof() di ogni colonna BLOB e lo riporta.
var Mappa = []Tabella{
	{Standalone: "users", Go: "utenti", Colonne: []Colonna{
		id,
		{Standalone: "email", Go: "email", Traduzione: Email},
		u("password_hash", "hash_password").nota("PBKDF2 dello standalone, si porta (OPS-02 per il Go); NULL = invito in attesa"),
		bool01("active", "attivo"),
		{Standalone: "role", Go: "ruolo", Traduzione: Ruolo},
		u("display_name", "nome_visibile"),
		ist("last_login_at", "ultimo_accesso_il"),
		ist("deactivated_at", "disattivato_il"),
		creato, aggiornato,
		u("session_version", "versione_sessione").nota("si porta il numero; i cookie dello standalone comunque non valgono nel Go"),
	}},
	{Standalone: "user_invitations", Go: "inviti_utenti", Colonne: []Colonna{
		id,
		u("user_id", "utente_id"),
		u("token_hash", "hash_token"),
		ist("expires_at", "scade_il"),
		ist("accepted_at", "accettato_il"),
		ist("revoked_at", "revocato_il"),
		creato, aggiornato,
	}},
	{Standalone: "password_resets", Go: "reset_password", Colonne: []Colonna{
		id,
		u("user_id", "utente_id"),
		u("token_hash", "hash_token"),
		ist("expires_at", "scade_il"),
		ist("used_at", "usato_il"),
		ist("revoked_at", "revocato_il"),
		u("ip", "ip"),
		u("channel", "canale").nota("«console» è del Go (D-04); lo standalone scrive telegram o email"),
		creato, aggiornato,
	}},
	{Standalone: "settings", Go: "impostazioni", Righe: "tutte le chiavi tranne ChiaviSegreteProvider", Colonne: []Colonna{
		u("key", "chiave").nota("la chiave resta quella dello standalone"),
		u("value", "valore"),
		ist("updated_at", "aggiornata_il"),
	}},
	{Standalone: "settings", Go: "segreti_provider", Righe: "solo le chiavi di ChiaviSegreteProvider (D-03)", Colonne: []Colonna{
		u("key", "nome"),
		u("value", "valore"),
		ist("updated_at", "aggiornato_il"),
	}},
	{Standalone: "audit_events", Go: "eventi_audit", Colonne: []Colonna{
		id,
		u("actor", "attore"),
		u("action", "azione"),
		u("resource", "risorsa"),
		u("resource_id", "id_risorsa"),
		u("outcome", "esito"),
		js("metadata", "dati"),
		u("ip", "ip"),
		u("user_agent", "user_agent"),
		creato,
	}},
	{Standalone: "shop_profiles", Go: "profilo_negozio", Colonne: []Colonna{
		id,
		u("scope", "ambito").nota("sempre 'default': una riga sola"),
		u("shop_name", "nome"),
		u("legal_name", "ragione_sociale"),
		u("address", "indirizzo"),
		u("postal_code", "cap"),
		u("city", "citta"),
		u("province", "provincia"),
		u("email", "email"),
		u("sales_email", "email_vendite"),
		u("phone", "telefono"),
		u("fiscal_id", "partita_iva").nota("nello standalone è la partita IVA (20261027200000)"),
		u("tax_code", "codice_fiscale"),
		u("warranty_terms", "condizioni_garanzia"),
		u("subtitle", "sottotitolo"),
		u("opening_hours", "orari"),
		u("instagram", "instagram"),
		u("facebook", "facebook"),
		u("website", "sito"),
		creato, aggiornato,
	}},
	{Standalone: "branding_assets", Go: "immagini_negozio", Colonne: []Colonna{
		id,
		u("kind", "tipo"),
		u("storage_key", "chiave"),
		u("original_filename", "nome_file"),
		u("mime_type", "tipo_mime"),
		u("byte_size", "byte"),
		u("sha256", "sha256"),
		ist("imported_at", "importata_il"),
	}},
	{Standalone: "customers", Go: "clienti", Colonne: []Colonna{
		id,
		u("name", "nome"),
		u("phone", "telefono"),
		creato, aggiornato,
	}},
	{Standalone: "bag_sequences", Go: "sequenze_buste", Colonne: []Colonna{
		u("year", "anno"),
		u("last_number", "ultimo_numero"),
	}},
	{Standalone: "oreficerie", Go: "oreficerie", Colonne: []Colonna{
		id.nota("la riga 1 è il negozio stesso (Oreficeria.is_b2b: id > 1 è un orefice)"),
		u("name", "nome"),
		u("phone", "telefono"),
	}},
	{Standalone: "artisans", Go: "artigiani", Colonne: []Colonna{
		id,
		u("name", "nome"),
		u("phone", "telefono"),
	}},
	{Standalone: "handovers", Go: "consegne_artigiani", Colonne: []Colonna{
		id,
		u("artisan_id", "artigiano_id"),
		ist("closed_at", "chiusa_il"),
		u("notes", "note"),
		creato, aggiornato,
	}},
	{Standalone: "ready_lots", Go: "lotti_avviso", Colonne: []Colonna{
		id,
		u("oreficeria_id", "oreficeria_id"),
		u("token", "token").nota("in chiaro come nello standalone: è il link /lotto"),
		ist("sent_at", "inviato_il"),
		ist("closed_at", "chiuso_il"),
		ist("public_access_disabled_at", "accesso_pubblico_spento_il"),
		creato, aggiornato,
	}},
	{Standalone: "repairs", Go: "buste", Colonne: []Colonna{
		id,
		u("customer_id", "cliente_id"),
		u("customer_name_snapshot", "nome_cliente"),
		u("customer_phone_snapshot", "telefono_cliente"),
		u("bag_number", "numero"),
		data("accepted_on", "data_accettazione"),
		data("expected_on", "data_prevista"),
		u("expected_on_text", "data_prevista_testo"),
		data("ready_on", "data_pronta"),
		data("delivered_on", "data_consegna"),
		u("description", "descrizione"),
		u("public_notes", "note_pubbliche"),
		u("internal_notes", "note_interne"),
		u("status", "stato").nota("la famiglia dipende da compro_oro; «annullata» è di tutte e due"),
		bool01("compro_oro", "compro_oro"),
		u("estimate_cents", "preventivo_centesimi"),
		u("estimate_text", "preventivo_testo"),
		u("final_price_cents", "prezzo_centesimi"),
		u("final_price_text", "prezzo_testo"),
		u("extra_days", "giorni_in_piu"),
		u("oreficeria_id", "oreficeria_id"),
		u("artisan_id", "artigiano_id"),
		u("handover_id", "consegna_artigiano_id"),
		u("ready_lot_id", "lotto_avviso_id"),
		u("public_token_hash", "hash_token_pubblico"),
		u("public_token", "token_pubblico").nota("in chiaro come nello standalone, per riusare il link nei messaggi (CLI-22)"),
		ist("public_token_expires_at", "token_pubblico_scade_il"),
		ist("public_token_revoked_at", "token_pubblico_revocato_il"),
		u("legacy_public_code_hash", "hash_codice_flask"),
		ist("legacy_public_revoked_at", "codice_flask_revocato_il"),
		ist("pinned_at", "fissata_il"),
		u("pinned_at_legacy_text", "fissata_il_testo_flask"),
		bool01("attesa_cliente", "attesa_cliente"),
		u("attesa_dal", "attesa_dal").nota("testo libero del Flask, non un istante"),
		u("attesa_motivo", "attesa_motivo"),
		u("qr_code", "codice_qr"),
		ist("handed_over_at", "consegnata_artigiano_il"),
		ist("returned_from_artisan_at", "rientrata_artigiano_il"),
		u("artisan_notes", "note_artigiano"),
		ist("last_message_at", "ultimo_messaggio_il"),
		ist("messages_read_at", "messaggi_letti_il"),
		u("messages_legacy_text", "messaggi_testo_flask"),
		ist("thread_visible_from", "messaggi_visibili_dal"),
		ist("ready_notified_at", "avviso_pronta_il"),
		ist("quote_approved_at", "preventivo_accettato_il"),
		ist("quote_approved_read_at", "preventivo_accettato_letto_il"),
		ist("quote_rejected_at", "preventivo_rifiutato_il"),
		ist("quote_rejected_read_at", "preventivo_rifiutato_letto_il"),
		u("quote_rejected_reason", "preventivo_rifiuto_motivo"),
		u("signature_storage_key", "firma_chiave"),
		ist("signature_at", "firma_il"),
		u("signature_collected_by", "firma_ritirata_da"),
		u("signature_byte_size", "firma_byte"),
		ist("overdue_notified_at", "ritardo_avvisato_il"),
		ist("chase_notified_at", "sollecito_avvisato_il"),
		ist("link_inviato_at", "link_inviato_il"),
		u("customer_email", "email_cliente").nota("uguale: nessuna normalizzazione, il consenso è legato al testo (CLI-39)"),
		ist("email_confirmed_at", "email_confermata_il"),
		ist("email_ready_sent_at", "email_pronta_inviata_il"),
		ist("retention_started_at", "conservazione_iniziata_il"),
		ist("deleted_at", "cancellata_il"),
		creato, aggiornato,
	}},
	{Standalone: "repair_status_versions", Go: "versioni_stato_buste", Colonne: []Colonna{
		u("repair_id", "busta_id"),
		u("version", "versione"),
	}},
	{Standalone: "repair_photos", Go: "foto_buste", Colonne: []Colonna{
		id,
		u("repair_id", "busta_id"),
		u("position", "posizione"),
		u("storage_key", "chiave"),
		u("original_filename", "nome_file"),
		u("mime_type", "tipo_mime"),
		u("byte_size", "byte"),
		u("sha256", "sha256"),
		bool01("is_bag", "foto_busta"),
		bool01("vis_customer", "visibile_cliente"),
		bool01("vis_artisan", "visibile_artigiano"),
		bool01("vis_orefice", "visibile_oreficeria"),
		ist("retention_compressed_at", "compressa_il"),
		creato, aggiornato,
	}},
	{Standalone: "repair_messages", Go: "messaggi_buste", Colonne: []Colonna{
		id,
		u("repair_id", "busta_id"),
		u("author", "autore"),
		u("channel", "canale"),
		u("body", "testo"),
		ist("sent_at", "inviato_il"),
		creato, aggiornato,
	}},
	{Standalone: "artisan_reminders", Go: "solleciti_artigiani", Colonne: []Colonna{
		id,
		u("repair_id", "busta_id"),
		u("artisan_id", "artigiano_id"),
		u("handover_id", "consegna_artigiano_id"),
		u("actor", "attore"),
		u("days_out", "giorni_fuori"),
		creato,
	}},
	{Standalone: "outgoing_messages", Go: "messaggi_in_uscita", Colonne: []Colonna{
		id,
		u("repair_id", "busta_id"),
		u("channel", "canale"),
		u("party", "destinatario"),
		u("address", "indirizzo"),
		u("subject", "oggetto"),
		u("body", "testo"),
		u("status", "stato").nota("le righe 'queued' restano ferme finché il titolare non decide (D-27)"),
		u("attempts", "tentativi"),
		ist("next_attempt_at", "prossimo_tentativo_il"),
		ist("queued_at", "accodato_il"),
		ist("accepted_at", "accettato_il"),
		ist("failed_at", "fallito_il"),
		u("provider_reference", "riferimento_provider"),
		u("last_error", "ultimo_errore"),
		u("parse_mode", "formato"),
		js("reply_markup", "tastiera"),
	}},
	{Standalone: "ready_notice_events", Go: "eventi_avviso_pronta", Colonne: []Colonna{
		id,
		u("repair_id", "busta_id"),
		u("event", "evento"),
		u("channel", "canale"),
		u("outgoing_message_id", "messaggio_in_uscita_id"),
		u("actor", "attore"),
		ist("occurred_at", "avvenuto_il"),
		creato, aggiornato,
	}},
	{Standalone: "quote_decision_events", Go: "eventi_preventivo", Colonne: []Colonna{
		id,
		u("repair_id", "busta_id"),
		u("event", "evento"),
		u("party", "parte"),
		u("reason", "motivo"),
		u("actor", "attore"),
		ist("occurred_at", "avvenuto_il"),
		creato, aggiornato,
	}},
	{Standalone: "delivery_signature_events", Go: "eventi_firma_consegna", Colonne: []Colonna{
		id,
		u("repair_id", "busta_id"),
		u("event", "evento"),
		u("storage_key", "chiave"),
		u("collected_by", "ritirata_da"),
		u("actor", "attore"),
		ist("occurred_at", "avvenuto_il"),
		creato, aggiornato,
	}},
	{Standalone: "telegram_subscriptions", Go: "iscrizioni_telegram", Colonne: []Colonna{
		id,
		u("repair_id", "busta_id"),
		u("chat_id", "chat_id"),
		bool01("consent", "consenso"),
		ist("linked_at", "collegata_il"),
		ist("consent_at", "consenso_il"),
		ist("stopped_at", "fermata_il"),
		u("stop_reason", "motivo_fermata"),
		creato, aggiornato,
	}},
	{Standalone: "telegram_link_tokens", Go: "token_telegram", Colonne: []Colonna{
		id,
		u("repair_id", "busta_id"),
		u("token_hash", "hash_token"),
		ist("expires_at", "scade_il"),
		creato,
	}},
	{Standalone: "dettati_addestramento", Go: "dettati_addestramento", Colonne: []Colonna{
		id,
		u("repair_id", "busta_id"),
		u("testo", "testo"),
		js("proposta", "proposta").nota("TEXT nella migrazione, :map nello schema Ecto: JSON"),
		u("modello", "modello"),
		creato,
	}},
	{Standalone: "dettati_esclusioni", Go: "dettati_esclusioni", Colonne: []Colonna{
		id,
		u("customer_id", "cliente_id"),
		creato,
	}},
	{Standalone: "courtesy_document_sequences", Go: "sequenze_documenti_cortesia", Colonne: []Colonna{
		u("year", "anno"),
		u("last_number", "ultimo_numero"),
	}},
	{Standalone: "sales", Go: "vendite", Colonne: []Colonna{
		id,
		data("sale_on", "data_vendita"),
		u("channel", "canale"),
		u("item_description", "descrizione"),
		u("serial_number", "numero_serie"),
		u("price_cents", "prezzo_centesimi"),
		u("price_text", "prezzo_testo"),
		u("warranty_months", "mesi_garanzia"),
		u("buyer_display_name", "acquirente_nome"),
		u("buyer_address", "acquirente_indirizzo"),
		u("buyer_postal_code", "acquirente_cap"),
		u("buyer_city", "acquirente_citta"),
		u("buyer_province", "acquirente_provincia"),
		u("buyer_email", "acquirente_email"),
		u("buyer_phone", "acquirente_telefono"),
		u("buyer_fiscal_id", "acquirente_id_fiscale"),
		u("internal_notes", "note_interne"),
		u("status", "stato"),
		u("document_year", "anno_documento"),
		u("document_number", "numero_documento"),
		ist("issued_at", "emessa_il"),
		data("closed_on", "data_chiusura"),
		u("closed_reason", "motivo_chiusura"),
		u("legacy_id", "id_flask"),
		ist("legacy_access_disabled_at", "accesso_flask_spento_il"),
		u("legacy_source_status", "stato_flask"),
		ist("deleted_at", "cancellata_il"),
		creato, aggiornato,
	}},
	{Standalone: "sale_revisions", Go: "revisioni_vendite", Colonne: []Colonna{
		id,
		u("sale_id", "vendita_id"),
		u("previous_revision_id", "revisione_precedente_id"),
		u("revision_number", "numero_revisione"),
		u("document_year", "anno_documento"),
		u("document_number", "numero_documento"),
		data("sale_on", "data_vendita"),
		u("channel", "canale"),
		u("item_description", "descrizione"),
		u("serial_number", "numero_serie"),
		u("price_cents", "prezzo_centesimi"),
		u("price_text", "prezzo_testo"),
		u("warranty_months", "mesi_garanzia"),
		data("warranty_start_on", "garanzia_dal"),
		data("warranty_end_on", "garanzia_al"),
		u("buyer_display_name", "acquirente_nome"),
		u("buyer_address", "acquirente_indirizzo"),
		u("buyer_postal_code", "acquirente_cap"),
		u("buyer_city", "acquirente_citta"),
		u("buyer_province", "acquirente_provincia"),
		u("buyer_email", "acquirente_email"),
		u("buyer_phone", "acquirente_telefono"),
		u("buyer_fiscal_id", "acquirente_id_fiscale"),
		u("seller_shop_name", "venditore_nome"),
		u("seller_legal_name", "venditore_ragione_sociale"),
		u("seller_address", "venditore_indirizzo"),
		u("seller_postal_code", "venditore_cap"),
		u("seller_city", "venditore_citta"),
		u("seller_province", "venditore_provincia"),
		u("seller_email", "venditore_email"),
		u("seller_sales_email", "venditore_email_vendite"),
		u("seller_phone", "venditore_telefono"),
		u("seller_fiscal_id", "venditore_partita_iva"),
		u("warranty_terms", "condizioni_garanzia"),
		u("public_token_hash", "hash_token_pubblico"),
		ist("public_token_expires_at", "token_pubblico_scade_il"),
		ist("issued_at", "emessa_il"),
		creato,
	}},
	{Standalone: "sale_photos", Go: "foto_vendite", Colonne: []Colonna{
		id,
		u("sale_id", "vendita_id"),
		u("position", "posizione"),
		u("storage_key", "chiave"),
		u("original_filename", "nome_file"),
		u("mime_type", "tipo_mime"),
		u("byte_size", "byte"),
		u("sha256", "sha256"),
		creato,
	}},
	{Standalone: "sale_revision_photos", Go: "foto_revisioni_vendite", Colonne: []Colonna{
		id,
		u("sale_revision_id", "revisione_vendita_id"),
		u("position", "posizione"),
		u("storage_key", "chiave"),
		u("original_filename", "nome_file"),
		u("mime_type", "tipo_mime"),
		u("byte_size", "byte"),
		u("sha256", "sha256"),
		creato,
	}},
	{Standalone: "sale_revision_seals", Go: "sigilli_revisioni_vendite", Colonne: []Colonna{
		id,
		u("sale_revision_id", "revisione_vendita_id"),
		ist("sealed_at", "sigillata_il"),
	}},
	{Standalone: "sale_revision_revocations", Go: "revoche_revisioni_vendite", Colonne: []Colonna{
		id,
		u("sale_revision_id", "revisione_vendita_id"),
		u("reason", "motivo"),
		ist("revoked_at", "revocata_il"),
	}},
	{Standalone: "emergency_events", Go: "eventi_emergenza", Colonne: []Colonna{
		id,
		ist("started_at", "iniziato_il"),
		ist("closed_at", "chiuso_il"),
		u("reason", "motivo"),
		u("recorder_serial", "matricola_registratore"),
		u("notes", "note"),
		u("opened_by", "aperto_da"),
		u("closed_by", "chiuso_da"),
		u("open_lock", "blocco_aperto").nota("sempre 1: l'indice unico parziale sugli eventi aperti ne lascia uno solo"),
		creato, aggiornato,
	}},
	{Standalone: "emergency_entries", Go: "righe_emergenza", Colonne: []Colonna{
		id,
		u("emergency_event_id", "evento_emergenza_id"),
		u("document_year", "anno_documento"),
		u("number", "numero"),
		u("description", "descrizione"),
		u("gross_cents", "lordo_centesimi"),
		u("vat_rate_percent", "aliquota_iva"),
		u("taxable_cents", "imponibile_centesimi"),
		u("vat_cents", "iva_centesimi"),
		u("payment_method", "pagamento"),
		u("customer_name", "nome_cliente"),
		u("repair_id", "busta_id"),
		u("reversal_of_id", "storno_di_id"),
		u("charge_round", "giro_addebito"),
		u("actor", "attore"),
		creato,
	}},
	{Standalone: "job_runs", Go: "esecuzioni_lavori", Colonne: []Colonna{
		id,
		u("job", "lavoro"),
		u("trigger", "avvio"),
		u("outcome", "esito"),
		ist("ran_at", "eseguito_il"),
		data("scheduled_for", "giorno").nota("il giorno del negozio, Europe/Rome"),
		u("notices", "avvisi"),
		u("messages", "messaggi"),
		u("detail", "dettaglio"),
	}},
	{Standalone: "media_deletion_jobs", Go: "cancellazioni_media", Colonne: []Colonna{
		u("storage_key", "chiave"),
		creato,
	}},
	{Standalone: "historical_documents", Go: "documenti_storici", Colonne: []Colonna{
		id,
		u("source_filename", "nome_file_flask"),
		u("storage_key", "chiave"),
		u("byte_size", "byte"),
		u("sha256", "sha256"),
		creato,
	}},
	{Standalone: "legacy_import_mappings", Go: "corrispondenze_import_flask", Nota: "provenienza dell'import Flask→standalone (OPS-04 di allora): si tiene come dati", Colonne: []Colonna{
		id,
		u("source_table", "tabella_flask"),
		u("source_id", "id_flask"),
		u("target_table", "tabella_standalone").nota("nomi delle tabelle dello standalone, lasciati così: è storia"),
		u("target_id", "id_standalone").nota("gli id dello standalone sono quelli del Go"),
		creato,
	}},
	{Standalone: "legacy_import_captures", Go: "cattura_import_flask", Nota: "la cattura del Flask da cui viene lo standalone (OPS-04 di allora): si tiene come dati", Colonne: []Colonna{
		u("id", "id").nota("sempre 1"),
		u("source_size", "byte_sorgente"),
		u("source_sha256", "sha256_sorgente"),
		js("sidecars_json", "file_accanto_json"),
		u("capture_sha256", "sha256_cattura"),
		creato,
	}},

	// Scartate.
	{Standalone: "permanent_deletion_permits", Nota: "nessun dato: le righe esistono solo dentro la transazione di " +
		"eliminazione definitiva e vi tornano indietro con essa, quindi in una copia a riposo è vuota (l'import controlla che lo sia). " +
		"Il meccanismo arriva con la sua area: RIP-S-53, VEN-15, VEN-18"},
	{Standalone: "schema_migrations", Nota: "le migrazioni di Ecto: il Go ha le sue (PIANO §1.4); la presenza della colonna version " +
		"è ciò che fa riconoscere un database dello standalone (db.Migra)"},
	{Standalone: "sqlite_sequence", Nota: "tabella interna di SQLite per AUTOINCREMENT: non si copia come tabella, ma l'import porta " +
		"il seq di ogni tabella a max(seq dello standalone, max id), così un id non si riusa mai (PIANO T2)"},
}

// TabelleSoloGo sono le tabelle del Go che non vengono dallo standalone, con il perché.
var TabelleSoloGo = map[string]string{
	"segreti": "la chiave del server del Go (slice 008a), creata al primo avvio; lo standalone tiene la sua " +
		"secret_key_base fuori dal database, e i cookie e i Phoenix.Token non si portano (D-02, D-31)",
}

// ColonneSoloGo sono le colonne del Go che non vengono dallo standalone, con il perché e il valore
// che l'import scrive.
var ColonneSoloGo = map[string]map[string]string{
	"reset_password": {
		"origine": "da dove è partito il link (D-04: la pagina o il comando locale); lo standalone ha solo la pagina, " +
			"quindi l'import scrive 'pagina'",
	},
}

// IndiciSoloGo sono gli indici del Go che lo standalone non ha, nella forma «tabella(colonne)»
// con « unico» e « parziale» dove vale, e il perché.
var IndiciSoloGo = map[string]string{}

// ElenchiSoloGo sono le colonne mappate che nel Go hanno un elenco chiuso di valori che il
// database dello standalone non controlla (lo controlla il suo codice), con il perché.
var ElenchiSoloGo = map[string]string{
	"reset_password.canale": "elenco della slice 011: telegram ed email come lo standalone, più console (D-04)",
}

// ChiaviSegreteProvider sono le chiavi di settings che contengono un segreto di un provider
// (Outgoing.Config e Assist.Config a go-baseline): vanno in segreti_provider, mai in
// impostazioni (D-03). Le altre chiavi di quei provider (server, porta, utente, chat…) non sono
// segrete e restano impostazioni.
var ChiaviSegreteProvider = []string{
	"outgoing.smtp.password",
	"outgoing.telegram.bot_token",
	"assist.ai.api_key",
	"assist.stt.api_key",
}

// Trigger sono i trigger dello standalone e la loro destinazione nel Go.
var Trigger = map[string]DestinoTrigger{
	// Immutabilità (VEN-15): portata subito, perché protegge i dati e non chiede logica. Senza
	// l'eccezione del permesso di eliminazione definitiva, che arriva con VEN-18 e RIP-S-53 (la
	// slice rifà questi trigger).
	"sale_revisions_no_update":                   {"revisioni_vendite_immutabili_modifica", "VEN-15, senza il permesso di eliminazione definitiva (VEN-18)"},
	"sale_revisions_no_delete":                   {"revisioni_vendite_immutabili_cancellazione", "VEN-15, senza il permesso di eliminazione definitiva (VEN-18)"},
	"sale_revision_photos_no_update":             {"foto_revisioni_vendite_immutabili_modifica", "VEN-15, come nello standalone"},
	"sale_revision_photos_no_delete":             {"foto_revisioni_vendite_immutabili_cancellazione", "VEN-15, senza il permesso di eliminazione definitiva (VEN-18)"},
	"sale_revision_photos_require_open_revision": {"foto_revisioni_vendite_solo_aperte", "VEN-15: niente foto su una revisione sigillata; l'import scrive le foto prima del sigillo"},
	"sale_revision_revocations_no_update":        {"revoche_revisioni_vendite_immutabili_modifica", "VEN-15, come nello standalone"},
	"sale_revision_revocations_no_delete":        {"revoche_revisioni_vendite_immutabili_cancellazione", "VEN-15, senza il permesso di eliminazione definitiva (VEN-18)"},
	"sale_revision_seals_no_update":              {"sigilli_revisioni_vendite_immutabili_modifica", "VEN-15, come nello standalone"},
	"sale_revision_seals_no_delete":              {"sigilli_revisioni_vendite_immutabili_cancellazione", "VEN-15, senza il permesso di eliminazione definitiva (VEN-18)"},
	"historical_documents_no_update":             {"documenti_storici_immutabili_modifica", "i PDF storici non cambiano, come nello standalone"},
	"historical_documents_no_delete":             {"documenti_storici_immutabili_cancellazione", "i PDF storici non si cancellano, come nello standalone"},
	"legacy_import_capture_immutable_update":     {"cattura_import_flask_immutabile_modifica", "la cattura del Flask non cambia, come nello standalone"},
	"legacy_import_capture_immutable_delete":     {"cattura_import_flask_immutabile_cancellazione", "la cattura del Flask non si cancella, come nello standalone"},

	// Regole di una riga sola: nel Go sono CHECK della tabella (stesso effetto su INSERT e UPDATE).
	"sale_photos_require_content_address":          {"", "CHECK chiave = lower(hex(sha256)) di foto_vendite"},
	"sale_photos_keep_content_address":             {"", "CHECK chiave = lower(hex(sha256)) di foto_vendite"},
	"sale_revision_photos_require_content_address": {"", "CHECK chiave = lower(hex(sha256)) di foto_revisioni_vendite"},
	"repair_photos_require_content_address":        {"", "CHECK chiave = lower(hex(sha256)) di foto_buste"},
	"repair_photos_keep_content_address":           {"", "CHECK chiave = lower(hex(sha256)) di foto_buste"},
	"historical_documents_require_content_address": {"", "CHECK chiave = lower(hex(sha256)) di documenti_storici"},
	"repairs_legacy_public_code_hash_size_insert":  {"", "CHECK length(hash_codice_flask) = 32 di buste"},
	"repairs_legacy_public_code_hash_size_update":  {"", "CHECK length(hash_codice_flask) = 32 di buste"},

	// Logica di un'area: arriva con la sua slice, che scrive anche i test.
	"sale_revisions_validate_chain":            {"", "rinviato a VEN-11 (emissione): confronta la revisione con la vendita e con il profilo negozio attuale, quindi l'import scrive le revisioni storiche prima che esista"},
	"sales_legacy_access_requires_seal_insert": {"", "rinviato a VEN-31 (abilitazione dei vecchi link dopo l'import)"},
	"sales_legacy_access_requires_seal_update": {"", "rinviato a VEN-31 (abilitazione dei vecchi link dopo l'import)"},
	"telegram_subscriptions_off_when_closed":   {"", "rinviato a CLI-50 (spegnimento automatico delle iscrizioni Telegram)"},
	"telegram_subscriptions_off_when_trashed":  {"", "rinviato a CLI-50 (spegnimento automatico delle iscrizioni Telegram)"},
	"telegram_subscriptions_off_when_b2b":      {"", "rinviato a CLI-50 (spegnimento automatico delle iscrizioni Telegram)"},
	"repair_status_versions_bump":              {"", "rinviato a RIP-S-13 (cambio di stato): finché il Go non cambia stati, il contatore importato basta"},
}
