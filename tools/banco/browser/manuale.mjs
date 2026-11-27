// Schermate del manuale d'uso (slice 049, docs/SVILUPPO.md «Schermate del manuale»). Gira nel
// container grgo-<nome>-manuale, montato al posto di banco.mjs nella stessa immagine del livello
// browser, sulla rete interna dell'istanza: fotografa SOLO il Go, con i dati sintetici, nelle due viste
// del livello browser (desktop 1440×1000, telefono 390×844). L'elenco delle schermate è
// $SCHERMATE (bench/manuale/schermate.json, letto e controllato anche da `banco manuale`); ogni voce
// ha una ricetta (sotto) che prepara la pagina in un contesto nuovo, con l'accesso del titolare
// sintetico, e un elemento da ritagliare (o la finestra intera). I PNG vanno in $USCITA; `banco` li
// copia in docs/manuale/img.
//
// Le ricette girano nell'ordine dell'elenco e alcune cambiano i dati (stampanti scelte, una busta
// nuova con le foto, un foglio di etichette mandato alla coda finta, una firma, le foto della galleria
// del capitolo 06): l'ordine dell'elenco conta. Le foto sono immagini inventate generate qui; le stampanti sono le code finte del banco
// (tools/banco/stampa.go). Le pagine si toccano in due punti soli, prima della foto: l'ora del timbro
// della firma, che cambia a ogni giro, si scrive fissa (ricetta firma-fatta); nei ritagli i pulsanti
// fissi si nascondono e la barra «← Indietro» non resta ferma in alto (ritaglio).
import {constants} from 'node:fs';
import {open, readFile} from 'node:fs/promises';
import {deflateSync, crc32} from 'node:zlib';
import {chromium} from 'playwright';

// Le stesse viste del livello browser (banco.mjs).
const VISTE = {
  desktop: {viewport: {width: 1440, height: 1000}},
  telefono: {viewport: {width: 390, height: 844}, isMobile: true, hasTouch: true, deviceScaleFactor: 2},
};

const attendi = ms => new Promise(r => setTimeout(r, ms));

function origine(valore) {
  const u = new URL(valore);
  if (u.protocol !== 'https:' || u.username || u.password || u.pathname !== '/' || u.search || u.hash) {
    throw new Error('origine non valida');
  }
  return u.origin;
}

// filePrivato legge un segreto sintetico, come banco.mjs.
async function filePrivato(percorso) {
  const f = await open(percorso, constants.O_RDONLY | constants.O_NOFOLLOW | constants.O_NONBLOCK);
  try {
    const st = await f.stat();
    if (!st.isFile() || (st.mode & 0o077) !== 0 || st.size < 1 || st.size > 4096) throw new Error('segreto non sicuro');
    const v = (await f.readFile('utf8')).replace(/\r?\n$/, '');
    if (!v || /[\r\n\u0000]/.test(v)) throw new Error('segreto non valido');
    return v;
  } finally {
    await f.close();
  }
}

// --- foto inventate (mai foto vere) ---------------------------------------------------------

function pezzoPNG(tipo, dati) {
  const lun = Buffer.alloc(4);
  lun.writeUInt32BE(dati.length);
  const td = Buffer.concat([Buffer.from(tipo, 'ascii'), dati]);
  const crc = Buffer.alloc(4);
  crc.writeUInt32BE(crc32(td) >>> 0);
  return Buffer.concat([lun, td, crc]);
}


// fotoOggetto è una foto inventata più tranquilla di quella del banco: uno sfondo a fasce morbide e un
// anello dorato, così le schermate restano leggere (pochi colori diversi) e somigliano a una foto di
// laboratorio. Il seme cambia sfondo e posizione.
function fotoOggetto(w, h, seme) {
  const righe = Buffer.alloc((w * 3 + 1) * h);
  const cx = w * (0.4 + (seme % 3) * 0.1), cy = h * 0.5, r1 = h * 0.22, r2 = h * 0.32;
  const fondo = [[226, 232, 240], [240, 230, 220], [222, 238, 228]][seme % 3];
  for (let y = 0; y < h; y++) {
    const o = y * (w * 3 + 1);
    const fascia = Math.floor((y / h) * 6) * 4;
    for (let x = 0; x < w; x++) {
      const d = Math.hypot(x - cx, y - cy);
      let c = fondo.map(v => v - fascia);
      if (d >= r1 && d <= r2) {
        const luce = Math.round((1 - Math.abs(d - (r1 + r2) / 2) / ((r2 - r1) / 2)) * 3) * 16;
        c = [190 + luce / 2, 150 + luce, 40 + luce / 2];
      }
      righe[o + 1 + x * 3] = c[0];
      righe[o + 2 + x * 3] = c[1];
      righe[o + 3 + x * 3] = c[2];
    }
  }
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(w, 0);
  ihdr.writeUInt32BE(h, 4);
  ihdr[8] = 8; ihdr[9] = 2;
  return Buffer.concat([Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), pezzoPNG('IHDR', ihdr),
    pezzoPNG('IDAT', deflateSync(righe)), pezzoPNG('IEND', Buffer.alloc(0))]);
}

const foto = (nome, seme) => ({name: nome, mimeType: 'image/png', buffer: fotoOggetto(640, 480, seme)});

// --- contesto -------------------------------------------------------------------------------

// contesto apre un contesto della vista con le sole richieste verso il Go (ogni altra si interrompe),
// ed entra come titolare sintetico.
async function contesto(browser, base, vista, cred) {
  const ctx = await browser.newContext({...VISTE[vista], serviceWorkers: 'block', locale: 'it-IT', timezoneId: 'Europe/Rome'});
  await ctx.route('**/*', route => (new URL(route.request().url()).origin === base ? route.fallback() : route.abort()));
  ctx.setDefaultTimeout(30000);
  const page = await ctx.newPage();
  await page.goto(`${base}/login`);
  await page.locator('[name="user[email]"]').fill(cred.email);
  await page.locator('[name="user[password]"]').fill(cred.password);
  await page.locator('form[action="/login"] button').click();
  await page.waitForURL(`${base}/`);
  return {ctx, page};
}

async function vai(page, base, percorso) {
  await page.goto(`${base}${percorso}`);
  await page.waitForLoadState('load');
}

// tratto disegna una firma sul riquadro: col mouse sul desktop, col dito sul telefono (eventi touch
// veri, con il dito fermo prima di alzarsi, come la ricetta firma di banco.mjs).
async function tratto(page, telefono) {
  const b = await page.locator('#signature-pad').boundingBox();
  const punti = [[0.15, 0.6], [0.25, 0.3], [0.35, 0.65], [0.45, 0.35], [0.55, 0.6], [0.65, 0.4], [0.8, 0.55]]
    .map(([x, y]) => [b.x + b.width * x, b.y + b.height * y]);
  if (telefono) {
    const cdp = await page.context().newCDPSession(page);
    const [x0, y0] = punti[0];
    await cdp.send('Input.dispatchTouchEvent', {type: 'touchStart', touchPoints: [{x: x0, y: y0}]});
    for (const [x, y] of punti.slice(1)) {
      await cdp.send('Input.dispatchTouchEvent', {type: 'touchMove', touchPoints: [{x, y}]});
      await attendi(30);
    }
    const [xf, yf] = punti[punti.length - 1];
    for (let i = 0; i < 5; i++) {
      await attendi(50);
      await cdp.send('Input.dispatchTouchEvent', {type: 'touchMove', touchPoints: [{x: xf, y: yf}]});
    }
    await cdp.send('Input.dispatchTouchEvent', {type: 'touchEnd', touchPoints: []});
    await attendi(300);
  } else {
    await page.mouse.move(...punti[0]);
    await page.mouse.down();
    for (const [x, y] of punti.slice(1)) await page.mouse.move(x, y, {steps: 4});
    await page.mouse.up();
  }
}

// Una busta nuova con due foto, come la fa il personale in «Nuova riparazione»: dati inventati.
// etichettaManuale è il numero dell'etichetta della prossima busta della ricetta: ogni busta ne ha una
// sua, perché dalla slice 053 una seconda busta aperta con la stessa etichetta riceve l'avviso delle
// gemelle invece di salvarsi (slice 055d).
let etichettaManuale = 900;

async function nuovaBustaConFoto(page, base) {
  await vai(page, base, '/nuova');
  await page.locator('#intake-photos').waitFor({state: 'visible'});
  await page.locator('[name="repair[customer_name]"]').fill('Cliente del manuale');
  await page.locator('[name="repair[description]"]').fill('Anello da ridimensionare');
  await page.locator('[name="repair[qr_code]"]').fill(`RIP-2026-${String(etichettaManuale++).padStart(4, '0')}`);
  await page.locator('#intake-gallery-input').setInputFiles([foto('anello-1.png', 3), foto('anello-2.png', 5)]);
  await page.locator('#intake-photo-status', {hasText: '2 foto pronte per la busta.'}).waitFor();
  await page.locator('#new-repair-form button[type=submit]').click();
  await page.locator('.flash-info', {hasText: 'salvata'}).first().waitFor();
}

// ritaglio è il riquadro che contiene tutti gli elementi del selettore (per esempio «.flash-info,
// main»: il messaggio sta fuori da main), sulla pagina intera, con 12 px intorno e tutta la larghezza
// dei contenuti che escono di lato (una riga più larga del suo riquadro resta intera). Prima i pulsanti
// fissi («🏠 Lista») si nascondono e la barra «← Indietro» smette di stare ferma in alto: in un ritaglio
// coprirebbero l'elemento (è uno dei due tocchi alla pagina, vedi in cima).
async function ritaglio(page, sel) {
  await page.addStyleTag({content: '.gs-fab, .gs-fab-spacer { display: none !important; } .gs-back-bar { position: static !important; }'});
  await page.evaluate(() => window.scrollTo(0, 0));
  await page.locator(sel).first().waitFor();
  return page.evaluate(s => {
    let sx = Infinity, su = Infinity, dx = -Infinity, giu = -Infinity;
    for (const el of document.querySelectorAll(s)) {
      const r = el.getBoundingClientRect();
      if (r.width === 0 && r.height === 0) continue;
      sx = Math.min(sx, r.left); su = Math.min(su, r.top); giu = Math.max(giu, r.bottom);
      dx = Math.max(dx, r.right);
      for (const d of el.querySelectorAll('*')) dx = Math.max(dx, d.getBoundingClientRect().right);
    }
    const larga = document.documentElement.scrollWidth;
    const x = Math.max(0, sx - 12), y = Math.max(0, su + window.scrollY - 12);
    return {x, y, width: Math.min(larga, dx + 12) - x, height: giu - su + 24};
  }, sel);
}

// immaginiCaricate aspetta (al più 30 s) che le immagini del selettore siano caricate e disegnabili.
async function immaginiCaricate(page, sel) {
  await page.waitForFunction(s => [...document.querySelectorAll(s)].every(i => i.complete && i.naturalWidth > 0), sel);
}

// --- capitolo 09, le oreficerie e i lotti (slice 099) ---------------------------------------
// Tutto con dati inventati: un'oreficeria «Oreficeria del manuale» e tre buste sue, pronte, create da
// «Nuova riparazione» come le fa il personale. Un lotto solo, preparato dalla prima ricetta che lo
// vuole (preparaLotto) e riusato dalle altre: due buste dentro e una ancora fra le «Pronte di … non
// ancora in lotto». Il lotto resta aperto: la pagina pubblica e lo storico si fotografano sul lotto 1
// dei dati sintetici, già inviato (token SINTETICO-banco-lotto-14-xxxxxxx). Le date si scrivono fisse (fissaOra).
const OREFICERIA_MANUALE = 'Oreficeria del manuale';
const GIORNO_FISSO = '15/03/2026';
const ORA_FISSA = '10:30';
let lottoManuale = null;

// assicuraOreficeria aggiunge da «Anagrafiche» l'oreficeria del manuale, se non c'è già. Se la aggiunge, la
// pagina resta con il messaggio «Oreficeria "…" aggiunta.».
async function assicuraOreficeria(page, base) {
  await vai(page, base, '/settings/anagrafiche');
  const esiste = await page.locator('#oreficerie-list input[name="oreficeria[name]"]')
    .evaluateAll((campi, nome) => campi.some(i => i.value === nome), OREFICERIA_MANUALE);
  if (esiste) return;
  await page.locator('#new-oreficeria-name').fill(OREFICERIA_MANUALE);
  await page.locator('#new-oreficeria-phone').fill('+39 000 000 0001');
  await page.locator('#new-oreficeria-form button[type=submit]').click();
  await page.locator('.flash-info', {hasText: 'aggiunta.'}).first().waitFor();
}

// fissaOra scrive fisse le date (gg/mm/aaaa, con l'ora se c'è) nei testi degli elementi del selettore: il lotto
// aperto dalla ricetta e quelli dei dati sintetici portano la data del giorno del giro, che cambierebbe a ogni
// giro (come l'ora della firma, ricetta firma-fatta). Tutte le date diventano le stesse, così le figure sono
// coerenti fra loro.
async function fissaOra(page, sel) {
  await page.evaluate(([s, giorno, ora]) => {
    for (const el of document.querySelectorAll(s)) {
      const w = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
      for (let n = w.nextNode(); n; n = w.nextNode()) {
        n.textContent = n.textContent.replace(/\d{2}\/\d{2}\/\d{4}( \d{2}:\d{2})?/g, (_, o) => giorno + (o ? ' ' + ora : ''));
      }
    }
  }, [sel, GIORNO_FISSO, ORA_FISSA]);
}

// preparaLotto crea tre buste pronte dell'oreficeria del manuale, apre il suo lotto e ci mette due buste
// (la prima scrivendo il numero, la seconda con «Aggiungi» dell'elenco). Dà il percorso del lotto e l'id di una
// busta che c'è dentro. Una volta sola per giro.
async function preparaLotto(page, base) {
  if (lottoManuale) return lottoManuale;
  await assicuraOreficeria(page, base);
  const numeri = [];
  for (const descrizione of ['Anello da ridimensionare', 'Collana da riparare', 'Orecchini senza chiusura']) {
    const n = etichettaManuale++;
    numeri.push(String(n).padStart(4, '0'));
    const codice = `RIP-2026-${numeri[numeri.length - 1]}`;
    await vai(page, base, '/nuova');
    await page.locator('[name="repair[customer_name]"]').fill('Cliente del manuale');
    await page.locator('[name="repair[description]"]').fill(descrizione);
    await page.locator('[name="repair[qr_code]"]').fill(codice);
    await page.locator('[name="repair[oreficeria_id]"]').selectOption({label: OREFICERIA_MANUALE});
    await page.locator('#new-repair-form button[type=submit]').click();
    await page.locator('.flash-info', {hasText: 'salvata'}).first().waitFor();
    // La scheda dal codice dell'etichetta (il riquadro della lista), poi «Pronta».
    await vai(page, base, '/');
    await page.locator('#repair-search-code').fill(codice);
    await page.locator('#repair-search-code').press('Enter');
    await page.locator('#qr-action').waitFor({state: 'visible'});
    await Promise.all([page.waitForURL(/\/repairs\/\d+$/), page.locator('#qr-btn-consulta').click()]);
    await page.waitForLoadState('load');
    await page.locator('#transition_status').selectOption('pronta');
    await page.locator('#repair-status-form button').click();
    await page.locator('#repair-notice').waitFor();
  }
  await vai(page, base, '/ready-lots');
  await page.locator('#ready-lot-goldsmith').selectOption({label: OREFICERIA_MANUALE});
  await Promise.all([page.waitForURL(/\/ready-lots\/\d+$/), page.locator('#ready-lot-open-form button[type=submit]').click()]);
  await page.waitForLoadState('load');
  const percorso = new URL(page.url()).pathname;
  const contaBuste = n => page.waitForFunction(x => (document.getElementById('ready-lot-bag-count') || {}).textContent === x, String(n));
  await page.locator('#ready-lot-bag-number').fill(`${numeri[0]}/26`);
  await page.locator('#ready-lot-add-form button[type=submit]').click();
  await contaBuste(1);
  await page.locator('#ready-lot-candidates button').first().click();
  await contaBuste(2);
  const id = await page.locator('#ready-lot-bags li').first().getAttribute('id');
  lottoManuale = {percorso, busta: Number(id.replace('ready-lot-bag-', ''))};
  return lottoManuale;
}

// --- capitolo 10, il dettato AI e la stima dei tempi (slice 118) ------------------------------
// Dati inventati. La stima vuole almeno 20 buste pronte in bottega negli ultimi 365 giorni: i dati sintetici non le
// hanno (le loro buste sono accettate a marzo, oltre i 90 giorni), quindi preparaStima ne crea 22 da «Nuova
// riparazione» con la data di ricezione qualche giorno prima del giro e le porta a «Pronta» il giorno del giro. Il
// dettato usa il modello finto del banco (modello.invalid, nella rete dell'istanza: lo raggiunge il Go, e lo script lo
// programma da Node come banco.mjs; il browser parla solo con il Go). La raccolta dei dettati è già accesa dal banco
// con una busta nel lotto (tools/banco/raccolta.go). L'ordine delle voci: prima «🔒 Dettato AI» (assistente non
// configurato), poi le ricette che lo configurano sul modello finto, per ultima quella che lo punta su 127.0.0.1 con una
// chiave (la figura della pagina «Assistente e dettatura»).
const MODELLO_FINTO = 'http://modello.invalid:8082';
let stimaManuale = null;
let assistenteManuale = false;

async function modelloFinto(percorso, risposte) {
  const r = await fetch(MODELLO_FINTO + percorso, {method: 'POST', headers: {'content-type': 'application/json'},
    body: JSON.stringify(risposte)});
  if (!r.ok) throw new Error(`modello finto: ${r.status}`);
}

// giornoRoma è la data (aaaa-mm-gg) di giorni fa, nel fuso del negozio.
const giornoRoma = giorni => new Intl.DateTimeFormat('sv-SE', {timeZone: 'Europe/Rome'}).format(new Date(Date.now() - giorni * 86400000));

// bustaNuova salva una busta da «Nuova riparazione» (dati inventati, un'etichetta sua) e dà l'id della scheda.
async function bustaNuova(page, base, {cliente, descrizione, ricevuta}) {
  const codice = `RIP-2026-${String(etichettaManuale++).padStart(4, '0')}`;
  await vai(page, base, '/nuova');
  await page.locator('#manual-intake').evaluate(d => { d.open = true; });
  await page.locator('[name="repair[customer_name]"]').fill(cliente);
  await page.locator('[name="repair[description]"]').fill(descrizione);
  if (ricevuta) await page.locator('#repair_accepted_on').fill(ricevuta);
  await page.locator('[name="repair[qr_code]"]').fill(codice);
  await page.locator('#new-repair-form button[type=submit]').click();
  await page.locator('.flash-info', {hasText: 'salvata'}).first().waitFor();
  await vai(page, base, '/');
  await page.locator('#repair-search-code').fill(codice);
  await page.locator('#repair-search-code').press('Enter');
  await page.locator('#qr-action').waitFor({state: 'visible'});
  await Promise.all([page.waitForURL(/\/repairs\/\d+$/), page.locator('#qr-btn-consulta').click()]);
  await page.waitForLoadState('load');
  return Number(new URL(page.url()).pathname.split('/').pop());
}

// preparaStima crea le 22 buste pronte (da 4 a 14 giorni dalla ricezione, tutte in bottega) e una busta in lavorazione
// con «Giorni in più» 4, di cui dà l'id. La stima resta spenta (come dopo un import), il supplemento 7-8. Una volta per giro.
async function preparaStima(page, base) {
  if (stimaManuale) return stimaManuale;
  const durate = [4, 5, 5, 6, 6, 6, 7, 7, 7, 7, 7, 8, 8, 8, 8, 9, 9, 10, 10, 11, 12, 14];
  const lavori = ['Anello da lucidare', 'Catenina da saldare', 'Orecchini da ripulire', 'Fede da allargare'];
  for (const [i, giorni] of durate.entries()) {
    await bustaNuova(page, base, {cliente: 'Cliente del manuale', descrizione: lavori[i % lavori.length], ricevuta: giornoRoma(giorni)});
    await page.locator('#transition_status').selectOption('pronta');
    await page.locator('#repair-status-form button').click();
    await page.locator('#repair-notice').waitFor();
  }
  const id = await bustaNuova(page, base, {cliente: 'Cliente del manuale', descrizione: 'Bracciale con maglia rotta da sostituire'});
  await vai(page, base, `/repairs/${id}?modifica=1`);
  await page.locator('#repair_extra_days').fill('4');
  await Promise.all([page.waitForURL(u => !u.searchParams.has('modifica')), page.locator('#save-repair').click()]);
  await page.waitForLoadState('load');
  stimaManuale = id;
  return id;
}

// configuraAssistente punta estrazione e trascrizione sul modello finto, senza chiave (una chiave in chiaro verso un altro
// host si rifiuta). Una volta per giro.
async function configuraAssistente(page, base) {
  if (assistenteManuale) return;
  await vai(page, base, '/settings/assistente');
  await page.locator('#ai_endpoint').fill(MODELLO_FINTO);
  await page.locator('#ai_model').fill('modello-finto');
  await page.locator('#ai-form button[type=submit]').click();
  await page.locator('.flash-info', {hasText: 'Impostazione salvata.'}).waitFor();
  await page.locator('#stt_endpoint').fill(MODELLO_FINTO);
  await page.locator('#stt-form button[type=submit]').click();
  await page.locator('.flash-info', {hasText: 'Impostazione salvata.'}).waitFor();
  assistenteManuale = true;
}

// estrai scrive il dettato, preme «✦ Estrai con AI» e aspetta l'esito riuscito.
async function estrai(page, dettato) {
  await page.locator('#assist-text').fill(dettato);
  await page.locator('#assist-estrai').click();
  await page.locator('#assist-status', {hasText: 'Dati estratti'}).waitFor();
}

// bustaDalDettato salva una busta dopo l'estrazione (la risposta predefinita del modello finto): la raccolta, se accesa, la
// prende.
async function bustaDalDettato(page, base) {
  await vai(page, base, '/nuova');
  await estrai(page, 'Mario Esempio, anello da lucidare, preventivo 30 euro');
  await page.locator('[name="repair[qr_code]"]').fill(`RIP-2026-${String(etichettaManuale++).padStart(4, '0')}`);
  await page.locator('#new-repair-form button[type=submit]').click();
  await page.locator('.flash-info', {hasText: 'salvata'}).first().waitFor();
}

// wavProva è un WAV inventato (1 s di un tono a 440 Hz, 16 kHz, mono): il trascrittore finto risponde con il testo
// programmato, la clip non si ascolta.
function wavProva() {
  const n = 16000, dati = Buffer.alloc(n * 2);
  for (let i = 0; i < n; i++) dati.writeInt16LE(Math.round(Math.sin(2 * Math.PI * 440 * i / 16000) * 8000), i * 2);
  const t = Buffer.alloc(44);
  t.write('RIFF', 0); t.writeUInt32LE(36 + dati.length, 4); t.write('WAVE', 8); t.write('fmt ', 12);
  t.writeUInt32LE(16, 16); t.writeUInt16LE(1, 20); t.writeUInt16LE(1, 22); t.writeUInt32LE(16000, 24);
  t.writeUInt32LE(32000, 28); t.writeUInt16LE(2, 32); t.writeUInt16LE(16, 34); t.write('data', 36); t.writeUInt32LE(dati.length, 40);
  return Buffer.concat([t, dati]);
}

// --- ricette --------------------------------------------------------------------------------
// Ognuna riceve la pagina già entrata e prepara quello che la schermata mostra.

const RICETTE = {
  // La scheda della busta così com'è.
  scheda: async (page, {base, busta}) => {
    await vai(page, base, `/repairs/${busta}`);
  },
  // «Nuova riparazione» con due foto scelte dalla Galleria, prima di «Genera busta».
  'nuova-foto': async (page, {base}) => {
    await vai(page, base, '/nuova');
    await page.locator('#intake-photos').waitFor({state: 'visible'});
    await page.locator('#intake-gallery-input').setInputFiles([foto('anello-fronte.png', 2), foto('anello-retro.png', 6)]);
    await page.locator('#intake-photo-status', {hasText: '2 foto pronte per la busta.'}).waitFor();
  },
  // Tre foto caricate dalla Galleria della scheda: la sezione «Foto (3)» dopo il caricamento.
  'foto-carica': async (page, {base, busta}) => {
    await vai(page, base, `/repairs/${busta}`);
    await page.locator('#gallery-upload input[type=file]').setInputFiles([foto('collana-1.png', 4), foto('collana-2.png', 7),
      foto('collana-3.png', 9)]);
    await page.locator('#repair-photos h2', {hasText: 'Foto (3)'}).waitFor();
    await immaginiCaricate(page, '#repair-photos .miniatura-foto img');
    await attendi(3500); // «Foto caricata.» sparisce dopo 3 s
  },
  // Il visualizzatore aperto sulla prima foto della busta.
  visualizzatore: async (page, {base, busta}) => {
    await vai(page, base, `/repairs/${busta}`);
    await page.locator('#repair-photos .miniatura-foto').first().click();
    await page.locator('.gs-viewer-frame:target img').waitFor();
    await immaginiCaricate(page, '.gs-viewer-frame:target img');
  },
  // La barretta della coda mentre la prima foto sta salendo: l'invio resta trattenuto, così la
  // barretta non cambia sotto la foto.
  'coda-in-corso': async (page, {base}) => {
    await page.route(u => /^\/repairs\/\d+\/photos$/.test(u.pathname), () => {});
    await nuovaBustaConFoto(page, base);
    // Subito la busta dopo: la barretta resta in basso anche in «Nuova riparazione».
    await page.locator('#new-repair-link').click();
    await page.locator('#intake-photos').waitFor({state: 'visible'});
    await page.locator('#photo-queue .riga-coda progress').waitFor();
    await attendi(500);
  },
  // La barretta dopo un invio non riuscito (connessione interrotta): «Riprova» e ✕. I tentativi da
  // soli dopo restano trattenuti.
  'coda-errore': async (page, {base}) => {
    let primo = true;
    await page.route(u => /^\/repairs\/\d+\/photos$/.test(u.pathname), route => {
      if (primo) { primo = false; return route.abort('failed'); }
    });
    await nuovaBustaConFoto(page, base);
    await page.locator('#photo-queue .riprova-coda').waitFor();
  },
  // Impostazioni → Stampanti con le code finte del banco: le etichette QR su Etichette_QR con due
  // opzioni, l'etichetta busta su Buste-76x127. Le scelte restano per le ricette dopo.
  stampanti: async (page, {base}) => {
    await vai(page, base, '/settings/stampanti');
    await page.locator('#printers-qr-queue').selectOption('Etichette_QR');
    await page.locator('#printers-qr-options').fill('MediaTracking=Gap Darkness=8');
    await page.locator('#printers-busta-queue').selectOption('Buste-76x127');
    await page.locator('#printers-save').click();
    await page.locator('.flash-info', {hasText: 'Stampanti salvate.'}).waitFor();
    await page.locator('#printers-qr details summary').click();
  },
  // Il modulo «Etichette QR».
  etichette: async (page, {base}) => {
    await vai(page, base, '/labels');
  },
  // La domanda prima della stampa diretta delle etichette QR.
  'etichette-domanda': async (page, {base}) => {
    await vai(page, base, '/labels');
    await page.locator('#label-sheet-print-button').click();
    await page.locator('#label-sheet-print-question').waitFor();
  },
  // Il foglio mandato alla coda finta: il modulo con l'esito e il contatore aggiornato.
  'etichette-inviate': async (page, {base}) => {
    await vai(page, base, '/labels');
    await page.locator('#label-sheet-print-button').click();
    await page.locator('#label-sheet-print-yes').click();
    await page.locator('.flash-info', {hasText: 'Inviate'}).waitFor();
  },
  // La domanda dell'azzeramento del contatore (il titolare).
  'etichette-azzera': async (page, {base}) => {
    await vai(page, base, '/labels');
    await page.locator('#label-sheet-reset-button').click();
    await page.locator('#label-sheet-reset-question').waitFor();
  },
  // La pagina di firma con la firma disegnata, prima di «Conferma firma».
  'firma-pagina': async (page, {base, busta, vista}) => {
    await vai(page, base, `/repairs/${busta}/firma`);
    await tratto(page, vista === 'telefono');
  },
  // La firma confermata: la scheda con la sezione della firma. L'ora del timbro si scrive fissa.
  'firma-fatta': async (page, {base, busta, vista}) => {
    await vai(page, base, `/repairs/${busta}/firma`);
    await tratto(page, vista === 'telefono');
    await page.locator('#signature-collected-by').fill('Figlia del cliente');
    await Promise.all([page.waitForURL(`${base}/repairs/${busta}`), page.locator('#signature-save').click()]);
    await page.waitForLoadState('load');
    await page.waitForFunction(() => {
      const img = document.getElementById('repair-signature-image');
      return img && img.complete && img.naturalWidth > 0;
    });
    await page.locator('#repair-signature-stamp').evaluate(el => {
      el.textContent = el.textContent.replace(/\d{2}\/\d{2}\/\d{4} \d{2}:\d{2}/, '15/03/2026 10:30');
    });
  },
  // --- capitolo 06, la lista e il QR (slice 059): leggono e non cambiano niente ---
  // La home così com'è: contatori, campo unico, cassetto dei filtri chiuso, le prime buste.
  lista: async (page, {base}) => {
    await vai(page, base, '/');
  },
  // I filtri attivi (le pillole) e il cassetto aperto.
  filtri: async (page, {base}) => {
    await vai(page, base, '/?scadenza=1&stato=in_lavorazione');
    await page.locator('#repair-filter-drawer summary').click();
    await page.locator('#repair-filter-form').waitFor({state: 'visible'});
  },
  // La galleria della home, con le miniature caricate. Prima tre foto inventate su due buste della
  // prima pagina (la 26/26 fissata e la 27/26): cambia i dati, e le ricette dopo non le guardano.
  galleria: async (page, {base}) => {
    for (const [busta, foto_] of [[26, [foto('bracciale-1.png', 11), foto('bracciale-2.png', 13)]], [27, [foto('orecchini.png', 14)]]]) {
      await vai(page, base, `/repairs/${busta}`);
      await page.locator('#gallery-upload input[type=file]').setInputFiles(foto_);
      await page.locator('#repair-photos h2', {hasText: `Foto (${foto_.length})`}).waitFor();
    }
    await vai(page, base, '/?vista=galleria');
    await page.locator('#repair-gallery').waitFor();
    await immaginiCaricate(page, '#repair-gallery img');
  },
  // La ricerca per numero d'etichetta «0045»: le gemelle 0045/26 e l'etichetta dell'anno prima.
  'cerca-etichetta': async (page, {base}) => {
    await vai(page, base, '/');
    await page.locator('#repair-search-code').fill('0045');
    await Promise.all([page.waitForLoadState('load'), page.locator('#repair-search-code').press('Enter')]);
    await page.locator('#active-repair-filters').waitFor();
  },
  // Il riquadro della busta sopra la lista, con il codice scritto come lo scrive il lettore a pistola
  // (con JavaScript il modulo parte con fetch e la lista resta sotto).
  'pannello-qr': async (page, {base}) => {
    await vai(page, base, '/');
    await page.locator('#repair-search-code').fill('RIP-2026-0047');
    await page.locator('#repair-search-code').press('Enter');
    await page.locator('#qr-lookup #qr-action').waitFor({state: 'visible'});
    await page.locator('#repairs').waitFor();
  },
  // Le gemelle di un'etichetta: la scelta nello stesso riquadro.
  'gemelle-qr': async (page, {base}) => {
    await vai(page, base, '/');
    await page.locator('#repair-search-code').fill('SINT-QR-0005');
    await page.locator('#repair-search-code').press('Enter');
    await page.locator('#qr-lookup #qr-matches-count').waitFor({state: 'visible'});
  },
  // La finestra della fotocamera dopo «🔄 Cambia fotocamera», con le due fotocamere finte di
  // Chromium (un disegno, nessun QR: la finestra resta aperta). Gira nel Chromium con le fotocamere.
  fotocamera: async (page, {base}) => {
    await vai(page, base, '/');
    await page.locator('#qr-lookup-scan').click();
    await page.locator('#qr-scanner-status', {hasText: 'Inquadra'}).waitFor();
    await page.locator('#qr-scanner-switch').click();
    await page.waitForFunction(() => {
      const v = document.querySelector('#qr-scanner-modal video');
      const t = v && v.srcObject && v.srcObject.getVideoTracks()[0];
      return t && t.readyState === 'live' && t.label === 'fake_device_1' && v.readyState >= 2;
    });
    await attendi(500);
  },
  // Il 📷 di «Etichetta» su un telefono senza fotocamera: il messaggio del browser nella finestra.
  'senza-fotocamera': async (page, {base}) => {
    await vai(page, base, '/nuova');
    await page.locator('#repair-qr-code-scan').click();
    await page.locator('#qr-scanner-status', {hasText: '⚠'}).waitFor();
  },
  // --- capitolo 09, le oreficerie e i lotti (slice 099) ---
  // «Anagrafiche» con l'oreficeria del manuale appena aggiunta e il suo messaggio. Prima delle altre del capitolo.
  'oreficerie-anagrafica': async (page, {base}) => {
    await assicuraOreficeria(page, base);
  },
  // La lista «Avviso oreficeria» con il lotto aperto oggi (lo prepara qui, la prima volta).
  'lotti-lista': async (page, {base}) => {
    await preparaLotto(page, base);
    await vai(page, base, '/ready-lots');
    await page.locator('#ready-lot-list li').first().waitFor();
    await fissaOra(page, '#ready-lot-list');
  },
  // La pagina del lotto aperto: due buste dentro, «Chiudi e invia» e una busta ancora da aggiungere.
  'lotto-pagina': async (page, {base}) => {
    const lotto = await preparaLotto(page, base);
    await vai(page, base, lotto.percorso);
    await page.locator('#ready-lot-send').waitFor();
    await fissaOra(page, 'main');
  },
  // La scheda di una busta del lotto: il riquadro «Avviso di ritiro» con «Apri il lotto».
  'scheda-in-lotto': async (page, {base}) => {
    const lotto = await preparaLotto(page, base);
    await vai(page, base, `/repairs/${lotto.busta}`);
    await page.locator('#repair-notice-in-lot').waitFor();
  },
  // Il dettaglio del lotto 1 dei dati sintetici, già inviato: link, «Reinvia all'oreficeria» e buste. Prima della
  // pagina pubblica: leggerla chiude il lotto e il reinvio sparisce.
  'storico-lotto': async (page, {base}) => {
    await vai(page, base, '/ready-lots/history/1');
    await page.locator('#ready-lot-resend').waitFor();
    await fissaOra(page, 'main');
  },
  // La pagina pubblica del lotto 1 dei dati sintetici, come la vede l'oreficeria (nessun accesso serve).
  'lotto-pubblico': async (page, {base}) => {
    await vai(page, base, '/lotto/SINTETICO-banco-lotto-14-xxxxxxx');
    await page.locator('#public-lot-greeting').waitFor();
    await fissaOra(page, 'main');
  },
  // --- capitolo 10, il dettato AI e la stima dei tempi (slice 118) ---
  // «Nuova riparazione» con l'assistente non ancora configurato: «🔒 Dettato AI» e «Inserimento riparazione».
  'dettato-spento': async (page, {base}) => {
    await vai(page, base, '/nuova');
    await page.locator('#assist-disabled').waitFor();
    await page.locator('#repair_accepted_on').fill('2026-03-15'); // la data di oggi, scritta fissa
  },
  // La scheda della busta in lavorazione con «Giorni in più» 4: l'intervallo, «su X buste» e la stima spenta per i clienti.
  'stima-scheda': async (page, {base}) => {
    const id = await preparaStima(page, base);
    await vai(page, base, `/repairs/${id}`);
    await page.locator('#repair-time-estimate', {hasText: 'su '}).waitFor();
    await fissaOra(page, '#repair-detail');
  },
  // La sezione «Stima tempi» di Impostazioni → «Negozio», con la stima spenta e il supplemento 7-8.
  'stima-impostazioni': async (page, {base}) => {
    await preparaStima(page, base);
    await vai(page, base, '/settings/shop');
    await page.locator('#estimate-today').waitFor();
  },
  // Il dettato estratto sul PC: un dettato inventato, la risposta programmata nel modello finto, il modulo aperto e
  // compilato. Le date dei campi si scrivono fisse (ricezione 15/03/2026, la consegna «fra dieci giorni» con la stessa
  // distanza).
  'dettato-estratto': async (page, {base}) => {
    await configuraAssistente(page, base);
    await vai(page, base, '/nuova');
    await modelloFinto('/_prova/risposte', [{contenuto: JSON.stringify({n: 'Giulia Esempio', b: '31',
      d: 'catenina in oro da saldare', c: 'fra dieci giorni', p: '35', a: '10'})}]);
    await estrai(page, 'Busta 31, Giulia Esempio 333 000 0001, catenina in oro da saldare, preventivo 35 euro, ' +
      'acconto 10, pronta fra dieci giorni');
    await page.evaluate(fisso => {
      const ric = document.getElementById('repair_accepted_on'), cons = document.getElementById('repair_expected_on');
      const d0 = Date.parse(ric.value);
      if (cons.value) cons.value = new Date(Date.parse(fisso) + Date.parse(cons.value) - d0).toISOString().slice(0, 10);
      ric.value = fisso;
    }, '2026-03-15');
  },
  // «Detta a voce» sul telefono: una riga scritta a mano e, in fondo, il testo del trascrittore finto (la clip è un
  // tono inventato, caricata nel campo della registrazione come la manda il pulsante).
  'dettato-voce': async (page, {base}) => {
    await configuraAssistente(page, base);
    await vai(page, base, '/nuova');
    await page.locator('#assist-mic').waitFor();
    await page.locator('#assist-text').fill('Busta 32, Luca Esempio 333 000 0002,');
    await modelloFinto('/_prova/risposte-stt', [{contenuto: 'fede in oro da allargare di una misura, pronta per sabato'}]);
    await page.locator('#assist-clip').setInputFiles({name: 'dettato.wav', mimeType: 'audio/wav', buffer: wavProva()});
    await page.locator('#assist-status', {hasText: 'Trascritto'}).waitFor();
  },
  // «Dettati per l'addestramento» accesa con un lotto in corso: una busta dal dettato chiude il lotto del banco (una busta
  // rimasta), «Avvia un nuovo lotto di 500 buste», un'altra busta dal dettato.
  'raccolta-dettati': async (page, {base}) => {
    await configuraAssistente(page, base);
    await bustaDalDettato(page, base);
    await vai(page, base, '/settings/assistente');
    page.on('dialog', d => d.accept());
    if (await page.locator('#training-refill').count()) {
      await page.locator('#training-refill').click();
      await page.locator('.flash-info', {hasText: 'Nuovo lotto avviato'}).waitFor();
    } else if (await page.locator('#training-on').count()) {
      await page.locator('#training-on').click();
      await page.locator('#training-off').waitFor();
    }
    await bustaDalDettato(page, base);
    await vai(page, base, '/settings/assistente');
    await page.locator('#training-off').waitFor();
  },
  // In fondo alla scheda del titolare: «🎙️ Escludi dalla raccolta dettati» (la raccolta è stata accesa).
  'escludi-cliente': async (page, {base, busta}) => {
    await vai(page, base, `/repairs/${busta}`);
    await page.locator('#repair-training-exclude').waitFor();
  },
  // --- capitolo 11, il backup (slice 132) ---
  // Il Go del banco non ha il file dell'apparecchio (niente chiavetta) e gira senza lavori automatici: la pagina
  // dice «non configurata» e il riquadro rosso tace. Niente server SFTP: si fotografa solo il primo passo della copia
  // remota. La copia isolata e la conferma vorrebbero un ripristino vero: restano a parole nel capitolo.
  // La pagina Impostazioni → Backup così com'è (il ritaglio salta la nota dei lavori spenti, solo del banco).
  'backup-pagina': async (page, {base}) => {
    await vai(page, base, '/settings/backup');
    await page.locator('#backup-status').waitFor();
    await page.locator('#backup-restore-test').waitFor();
  },
  // La sezione «Copia remota» prima della chiave dell'apparecchio: «Crea la chiave dell'apparecchio» e il primo passo.
  'copia-remota': async (page, {base}) => {
    await vai(page, base, '/settings/backup');
    await page.locator('#backup-remote-key-button').waitFor();
    await page.locator('#backup-remote-form').waitFor();
  },
  // «Crea la chiave dell'apparecchio» premuto (cambia i dati: dopo, l'apparecchio del banco ha la sua chiave SFTP). La
  // chiave nasce nuova a ogni giro: nella casella si scrive fissa una riga inventata, come l'ora della firma.
  'copia-remota-chiave': async (page, {base}) => {
    await vai(page, base, '/settings/backup');
    if (await page.locator('#backup-remote-key-button').count()) {
      await page.locator('#backup-remote-key-button').click();
    }
    await page.locator('#backup-remote-pubkey').waitFor();
    await page.locator('#backup-remote-pubkey').evaluate(el => {
      el.value = 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIChiaveInventataDelManualeNonVera0000000000 gestrip-apparecchio';
    });
  },
  // «Assistente e dettatura» come la prepara chi installa: i due servizi sulla stessa macchina (127.0.0.1) con una chiave
  // inventata, salvata (la casella resta vuota), e qualche parola del negozio. Per ultima: dopo, il dettato non risponde più.
  'assistente-configurato': async (page, {base}) => {
    await vai(page, base, '/settings/assistente');
    await page.locator('#ai_endpoint').fill('http://127.0.0.1:8081');
    await page.locator('#ai_model').fill('estrattore-buste');
    await page.locator('#ai_api_key').fill('chiave-inventata-del-manuale');
    await page.locator('#ai-form button[type=submit]').click();
    await page.locator('.flash-info', {hasText: 'Impostazione salvata.'}).waitFor();
    await page.locator('#stt_endpoint').fill('http://127.0.0.1:8082');
    await page.locator('#stt_api_key').fill('chiave-inventata-del-manuale');
    await page.locator('#stt_glossary').fill('maglia veneziana\nchiusura a moschettone\nOreficeria del manuale');
    await page.locator('#stt-form button[type=submit]').click();
    await page.locator('.flash-info', {hasText: 'Impostazione salvata.'}).waitFor();
    await vai(page, base, '/settings/assistente');
    await page.locator('#ai-state', {hasText: 'Configurato'}).waitFor();
  },
};

// Le ricette che vogliono la fotocamera finta: girano in un Chromium a parte, con due fotocamere
// finte (il disegno di Chromium, senza QR) e il permesso già dato. Il Chromium normale non ha
// nessuna fotocamera (la ricetta senza-fotocamera).
const CON_FOTOCAMERE = new Set(['fotocamera']);

// --- giro -----------------------------------------------------------------------------------

async function main() {
  const base = origine(process.env.GO_URL);
  const elenco = JSON.parse(await readFile(process.env.SCHERMATE, 'utf8')).schermate;
  for (const s of elenco) if (!RICETTE[s.ricetta]) throw new Error(`ricetta sconosciuta: ${s.ricetta}`);
  const cred = {email: await filePrivato('/banco/cred/email'), password: await filePrivato('/banco/cred/password')};
  // --lang: i controlli del browser (il campo file, le date) in italiano, come sul telefono del negozio.
  const browser = await chromium.launch({headless: true, chromiumSandbox: true, args: ['--lang=it-IT']});
  let browserCamere = null;
  let errori = 0;
  try {
    for (const s of elenco) {
      const camere = CON_FOTOCAMERE.has(s.ricetta);
      if (camere && !browserCamere) {
        browserCamere = await chromium.launch({headless: true, chromiumSandbox: true, args: ['--lang=it-IT',
          '--use-fake-device-for-media-stream=device-count=2', '--use-fake-ui-for-media-stream']});
      }
      const {ctx, page} = await contesto(camere ? browserCamere : browser, base, s.vista, cred);
      if (camere) await ctx.grantPermissions(['camera'], {origin: base});
      try {
        await RICETTE[s.ricetta](page, {base, busta: s.busta, vista: s.vista});
        // Il cursore del campo attivo e le animazioni non devono cambiare la foto.
        await page.evaluate(() => document.activeElement && document.activeElement.blur && document.activeElement.blur());
        const opz = {animations: 'disabled', caret: 'hide', path: `${process.env.USCITA}/${s.file}`};
        if (s.elemento) await page.screenshot({...opz, fullPage: true, clip: await ritaglio(page, s.elemento)});
        else await page.screenshot(opz);
        console.log('fatta', s.file);
      } catch (e) {
        errori++;
        console.log('ERRORE', s.file, `(${s.ricetta})`, e && e.name, String(e && e.message || e).split('\n')[0].replace(/https:\/\/[^\s/]+/g, '<app>'));
      } finally {
        await ctx.close().catch(() => {});
      }
    }
  } finally {
    await browser.close().catch(() => {});
    if (browserCamere) await browserCamere.close().catch(() => {});
  }
  if (errori > 0) process.exit(1);
}

main().catch(e => { console.error('manuale:', e && e.name, String(e && e.message || e).split('\n')[0]); process.exit(1); });
