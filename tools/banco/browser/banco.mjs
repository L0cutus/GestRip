// Livello browser del banco a scatola chiusa (slice 040a, PIANO §4.3). Gira nel container
// grgo-<nome>-browser, sulla rete interna dell'istanza, e fa le STESSE azioni sullo standalone e sul
// Go con Chromium (Playwright 1.58.2), nelle due viste: desktop 1440×1000 e telefono 390×844.
// Non confronta niente: scrive le osservazioni (campo → valore) per app, scenario e vista in
// $USCITA; il confronto e le attese li fa `banco` (browser.go), come per il livello HTTP.
//
// Solo istanze sintetiche: le foto si generano qui (immagini inventate), le credenziali sono quelle
// sintetiche dell'istanza, lette da file privati. Nessun indirizzo fuori dalle due app.
import {constants} from 'node:fs';
import {open, writeFile} from 'node:fs/promises';
import {deflateSync, crc32} from 'node:zlib';
import {chromium} from 'playwright';

const ADESSO = () => Date.now();
// fase è il punto dello scenario in corso: finisce nell'errore, per sapere dove si è fermato.
let fase = '';
const fa = f => { fase = f; };
const NESSUNO = '(nessuno)';

// --- ambiente -------------------------------------------------------------------------------

function origine(valore) {
  const u = new URL(valore);
  if (u.protocol !== 'https:' || u.username || u.password || u.pathname !== '/' || u.search || u.hash) {
    throw new Error('origine non valida');
  }
  return u.origin;
}

// filePrivato legge un segreto sintetico: file normale, solo del proprietario, piccolo, una riga.
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

// --- foto inventate -------------------------------------------------------------------------

function pezzoPNG(tipo, dati) {
  const lun = Buffer.alloc(4);
  lun.writeUInt32BE(dati.length);
  const td = Buffer.concat([Buffer.from(tipo, 'ascii'), dati]);
  const crc = Buffer.alloc(4);
  crc.writeUInt32BE(crc32(td) >>> 0);
  return Buffer.concat([lun, td, crc]);
}

// fotoPNG è un PNG RGB w×h con un disegno che dipende da seme: niente foto vere (PIANO §4.6).
function fotoPNG(w, h, seme) {
  const righe = Buffer.alloc((w * 3 + 1) * h);
  for (let y = 0; y < h; y++) {
    const o = y * (w * 3 + 1);
    for (let x = 0; x < w; x++) {
      righe[o + 1 + x * 3] = (x * 7 + seme * 40) & 255;
      righe[o + 2 + x * 3] = (y * 5 + seme * 90) & 255;
      righe[o + 3 + x * 3] = ((x ^ y) + seme * 13) & 255;
    }
  }
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(w, 0);
  ihdr.writeUInt32BE(h, 4);
  ihdr[8] = 8; ihdr[9] = 2;
  return Buffer.concat([Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), pezzoPNG('IHDR', ihdr),
    pezzoPNG('IDAT', deflateSync(righe)), pezzoPNG('IEND', Buffer.alloc(0))]);
}

const foto = (nome, seme) => ({name: nome, mimeType: 'image/png', buffer: fotoPNG(320, 240, seme)});
// Oltre 25 MiB di un byte: il limite si misura prima di guardare il contenuto, dalle due parti.
const fotoGrande = () => {
  const b = Buffer.alloc(25 * 1024 * 1024 + 1, 0x41);
  fotoPNG(8, 8, 1).copy(b);
  return {name: 'grande-040a.png', mimeType: 'image/png', buffer: b};
};

// --- pagina ---------------------------------------------------------------------------------

const VISTE = {
  desktop: {viewport: {width: 1440, height: 1000}},
  telefono: {viewport: {width: 390, height: 844}, isMobile: true, hasTouch: true, deviceScaleFactor: 2},
};

// Osservato da ogni pagina caricata (anche dopo un invio del modulo), attivo prima di ogni azione:
// un'anteprima della foto scelta (live_img_preview) e una barra di avanzamento (entry.progress), viste
// anche se compaiono e spariscono nello stesso istante (si guardano i nodi aggiunti di ogni
// mutazione, non solo la pagina che resta); i testi del riquadro degli errori del caricamento; i
// messaggi, con l'istante in cui compaiono e spariscono (Date.now() della pagina).
const OSSERVATORE = `(() => {
  const segnala = (cosa, valore) => window.__segnala && window.__segnala(cosa, valore || '', Date.now());
  const testo = el => (el.textContent || '').replace(/\\s+/g, ' ').trim();
  const ANTEPRIMA = 'img[alt^="Anteprima di"]', BARRA = '#repair-photos progress, #repair-photo-upload-progress progress';
  const nodo = (n, sel) => n.nodeType === 1 && (n.matches(sel) || n.querySelector(sel));
  let presenti = new Set(), titolo = null;
  const guarda = record => {
    const h = document.querySelector('#repair-photos h2');
    if (h && testo(h) !== titolo) { titolo = testo(h); segnala('titolo-foto', titolo); }
    for (const r of record || []) {
      for (const n of r.addedNodes) {
        if (nodo(n, ANTEPRIMA)) segnala('anteprima');
        if (nodo(n, BARRA)) segnala('avanzamento');
      }
    }
    if (document.querySelector(ANTEPRIMA)) segnala('anteprima');
    if (document.querySelector(BARRA)) segnala('avanzamento');
    const e = document.querySelector('#repair-photo-upload-errors');
    if (e && testo(e)) segnala('errori-foto', testo(e));
    const ora = new Set();
    document.querySelectorAll('#flash-info, .flash-info').forEach(m => {
      if (testo(m) && m.getClientRects().length > 0) ora.add(testo(m));
    });
    for (const t of ora) if (!presenti.has(t)) { segnala('messaggio', t); segnala('messaggio-compare', t); }
    for (const t of presenti) if (!ora.has(t)) segnala('messaggio-via', t);
    presenti = ora;
  };
  guarda();
  new MutationObserver(guarda).observe(document, {subtree: true, childList: true, characterData: true, attributes: true});
})()`;

// guasti della pagina, per app e vista: errori JavaScript (pageerror ed errori della console),
// risposte 4xx e 5xx, richieste e redirect fuori dall'origine (interrotti). Gli indirizzi si
// scrivono senza origine e senza query.
function senzaOrigine(t) {
  return String(t).replace(/https?:\/\/[^\s/'")]+/g, '').replace(/\?[^\s'")]*/g, '').slice(0, 160);
}

async function contesto(browser, base, vista) {
  const ctx = await browser.newContext({...VISTE[vista], ignoreHTTPSErrors: false, serviceWorkers: 'block',
    locale: 'it-IT', timezoneId: 'Europe/Rome'});
  ctx.guasti = {js: [], risposte: [], fuori: 0};
  // Le foto della coda che arrivano alla rete, da qualunque pagina del contesto (scenario sfondo).
  ctx.inviiFoto = 0;
  ctx.idInvii = [];
  // Solo l'origine dell'app sotto prova, come browser/activity-087.mjs dello standalone: ogni altra
  // richiesta si interrompe, i redirect non si seguono da soli e uno verso un'altra origine si
  // interrompe anche lui.
  // Gli indirizzi wa.me che una pagina apre (scenario whatsapp, slice 066): si annotano e si
  // interrompono, mai raggiunti, e non contano come richieste fuori dall'origine.
  ctx.waMe = [];
  // Ogni richiesta verso wa.me si annota una volta sola, anche se la vedono sia la rotta sia l'evento
  // request (review della 068: le aperture si contano senza togliere i doppioni).
  ctx.richiesteWa = new WeakSet();
  const annotaWa = r => { if (!ctx.richiesteWa.has(r)) { ctx.richiesteWa.add(r); ctx.waMe.push(r.url()); } };
  await ctx.route('**/*', async route => {
    try {
      if (route.request().url().startsWith('https://wa.me/')) { annotaWa(route.request()); return await route.abort(); }
      if (new URL(route.request().url()).origin !== base) { ctx.guasti.fuori++; return await route.abort(); }
      if (route.request().method() === 'POST' && /^\/repairs\/\d+\/photos$/.test(new URL(route.request().url()).pathname)) {
        ctx.inviiFoto++;
        ctx.idInvii.push(route.request().headers()['x-upload-id'] || '');
      }
      // Le azioni del pannello del QR del Go che arrivano alla rete (scenario pannello-qr: un doppio
      // tocco manda il modulo una volta sola). Lo standalone non ne ha: sono eventi LiveView.
      if (route.request().method() === 'POST' && /^\/repairs\/\d+\/qr-(avanza|preventivo)$/.test(new URL(route.request().url()).pathname)) {
        ctx.inviiQR = (ctx.inviiQR || 0) + 1;
      }
      const risposta = await route.fetch({maxRedirects: 0, timeout: 90000});
      const location = risposta.headers().location;
      if (location && new URL(location, route.request().url()).origin !== base) { ctx.guasti.fuori++; return await route.abort(); }
      // Uno scenario può cambiare il corpo della pagina a cui porta il redirect di una richiesta
      // (ctx.trasforma, solo lo scenario coda): Chromium segue da sé il redirect di una fetch senza
      // ripassare dalle rotte, quindi qui si segue il redirect e si risponde con la pagina cambiata.
      const cambia = ctx.trasforma && location && ctx.trasforma(route.request());
      if (cambia) {
        const h = {...route.request().headers()};
        delete h['content-type'];
        delete h['content-length'];
        const seguito = await route.fetch({url: new URL(location, route.request().url()).href, method: 'GET', headers: h,
          postData: '', maxRedirects: 0, timeout: 90000});
        return await route.fulfill({response: seguito, body: cambia(await seguito.text())});
      }
      await route.fulfill({response: risposta});
    } catch {
      ctx.guasti.fuori++;
      await route.abort().catch(() => {});
    }
  });
  await ctx.routeWebSocket('**/*', ws => {
    const u = new URL(ws.url());
    if (u.protocol === 'wss:' && `https://${u.host}` === base) ws.connectToServer(); else { ctx.guasti.fuori++; ws.close(); }
  });
  // Una scheda aperta da window.open va a wa.me con una navigazione sua: la si vede anche dalla
  // richiesta, se la rotta non la prende (slice 066).
  ctx.on('request', r => { if (r.url().startsWith('https://wa.me/')) annotaWa(r); });
  ctx.on('page', page => {
    page.on('framenavigated', f => { if (f === page.mainFrame() && f.url().startsWith('https://wa.me/') && !ctx.waMe.includes(f.url())) ctx.waMe.push(f.url()); });
    page.on('pageerror', e => ctx.guasti.js.push(`pageerror: ${senzaOrigine(e && e.message)}`));
    // «Failed to load resource: the server responded with a status of …» è la stessa risposta 4xx o
    // 5xx che pagina.risposte-errore registra già: qui non si conta due volte.
    page.on('console', m => {
      if (m.type() !== 'error' || /^Failed to load resource: the server responded with a status of \d+/.test(m.text())) return;
      ctx.guasti.js.push(`console: ${senzaOrigine(m.text())}`);
    });
    page.on('response', r => {
      if (r.status() >= 400) ctx.guasti.risposte.push(`${r.status()} ${r.request().method()} ${new URL(r.url()).pathname}`);
    });
  });
  ctx.segnali = new Set();
  ctx.eventi = [];
  await ctx.exposeBinding('__segnala', (_s, cosa, valore, t) => {
    ctx.segnali.add(valore ? `${cosa}: ${valore}` : cosa);
    ctx.eventi.push({cosa, valore, t});
  });
  await ctx.addInitScript(`document.addEventListener('DOMContentLoaded', () => ${OSSERVATORE})`);
  ctx.setDefaultTimeout(30000);
  return ctx;
}

function azzeraGuasti(ctx) {
  ctx.guasti = {js: [], risposte: [], fuori: 0};
}

function scriviGuasti(ctx, o) {
  const elenco = l => (l.length ? [...new Set(l)].sort().join(' | ') : NESSUNO);
  o['pagina.errori-js'] = elenco(ctx.guasti.js);
  o['pagina.risposte-errore'] = elenco(ctx.guasti.risposte);
  o['pagina.fuori-origine'] = String(ctx.guasti.fuori);
}

// pronta aspetta la pagina utilizzabile: lo standalone quando LiveView è collegata, il Go quando è
// caricata. È l'unica cosa che il banco fa diversa fra le due app.
async function pronta(page, app) {
  if (app === 'standalone') await page.locator('.phx-connected').first().waitFor();
  else await page.waitForLoadState('load');
}

async function accedi(ctx, base, app, cred) {
  const page = await ctx.newPage();
  await page.goto(`${base}/login`);
  await page.locator('[name="user[email]"]').fill(cred.email);
  await page.locator('[name="user[password]"]').fill(cred.password);
  await page.locator('form[action="/login"] button').click();
  await page.waitForURL(`${base}/`);
  await pronta(page, app);
  await page.close();
}

const testo = s => (s || '').replace(/\s+/g, ' ').trim();
const SEL_INFO = '#flash-info, .flash-info';
const SEL_ERRORE = '#flash-error, .flash-errore';

async function leggi(page, sel) {
  const loc = page.locator(sel);
  const n = await loc.count();
  if (n === 0) return NESSUNO;
  const parti = [];
  for (let i = 0; i < n; i++) if (await loc.nth(i).isVisible()) parti.push(testo(await loc.nth(i).innerText()));
  return parti.length ? parti.join(' | ') : NESSUNO;
}
const messaggio = page => leggi(page, SEL_INFO);
const errore = page => leggi(page, SEL_ERRORE);
const titoloFoto = async page => testo(await page.locator('#repair-photos h2').innerText());
const percorso = page => new URL(page.url()).pathname;
// visti sono i testi di un tipo passati dalla pagina dall'ultimo azzeramento, in ordine alfabetico.
const visti = (page, cosa) => [...page.context().segnali].filter(x => x.startsWith(`${cosa}: `))
  .map(x => x.slice(cosa.length + 2)).sort().join(' | ') || NESSUNO;
// aspettaEvento aspetta (al più ms) un evento dell'osservatore dall'indice da in poi.
async function aspettaEvento(ctx, da, cosa, valore, ms) {
  const fine = ADESSO() + ms;
  for (;;) {
    const i = ctx.eventi.findIndex((x, n) => n >= da && x.cosa === cosa && x.valore === valore);
    if (i >= 0) return {...ctx.eventi[i], i};
    if (ADESSO() > fine) return null;
    await attendi(50);
  }
}

// durata fra due eventi della pagina: «circa 3 s» fra 2,5 e 4,5 s (il messaggio di LiveView e quello
// di app.js spariscono dopo 3 s), altrimenti la misura.
function durata(da, a) {
  if (!da) return 'inizio non visto';
  if (!a) return 'non sparisce entro 9 s';
  const ms = a.t - da.t;
  return ms >= 2500 && ms <= 4500 ? 'circa 3 s' : `${(ms / 1000).toFixed(1)} s`;
}
// Il messaggio dopo «Genera busta»: «Riparazione 45/26 creata.» nello standalone, «Busta 0045/26
// salvata» (o «Busta N. 45/26 salvata») nel Go (D-64, studio punto (d), slice 053).
const SALVATA = /creata\.|salvata/;
const NUMERO_SALVATA = /Riparazione (.+?) creata\.|Busta (.+?) salvata/;
const attendi = ms => new Promise(r => setTimeout(r, ms));

// numeroDel2025 scrive il numero della busta nel campo «N° Busta» dello standalone (il Go non l'ha, D-64), per gli
// scenari che creano buste loro (dati propri, SVILUPPO.md): lo standalone lo precompila con il numero dell'anno in
// corso (peek_next_bag_number/0) e lo usa com'è, quindi la data di accettazione del 2025 da sola non basta e la busta
// prenderebbe un numero del 2026, spostando le liste e la numerazione che gli altri scenari leggono (banco s082p:
// riquadro-azioni sul telefono). Il Go numera con l'anno della data di accettazione (accepted_on_year/1, come lo
// standalone a campo vuoto). Il numero non si confronta.
async function numeroDel2025(page, numero) {
  const campo = page.locator('[name="repair[bag_number]"]');
  if (await campo.count()) await campo.fill(numero);
}

async function apriScheda(page, base, app, id) {
  await page.goto(`${base}/repairs/${id}`);
  await pronta(page, app);
}

// fino aspetta che il titolo delle foto dica n foto (anche dopo un invio che ricarica la pagina).
async function fotoDiventano(page, n) {
  await page.locator('#repair-photos h2', {hasText: `Foto (${n})`}).waitFor();
}
const numeroFoto = async page => Number((await titoloFoto(page)).match(/\((\d+)\)/)[1]);

// Su database vuoto (solo i due account) ogni busta dello scenario non c'è: le due app tornano alla
// home con il loro messaggio, come gli scenari HTTP.
async function bustaAssente(page, base, app, id, o, chiave) {
  await page.goto(`${base}/repairs/${id}`);
  await pronta(page, app);
  o[`${chiave}.percorso`] = percorso(page);
  o[`${chiave}.errore`] = await errore(page);
}

// --- scenari --------------------------------------------------------------------------------

// Il visualizzatore delle foto della scheda (RIP-S-28): apertura da una miniatura, barra, tasti,
// Esc, scorrimento col dito (telefono) e cronologia (le mosse della barra sostituiscono la voce).
async function visualizzatore(page, {base, app, vista, dati}, o) {
  if (!dati) return bustaAssente(page, base, app, 1, o, 'busta-1');
  await page.goto(`${base}/`);
  await pronta(page, app);
  await apriScheda(page, base, app, 1);
  o['foto'] = await titoloFoto(page);
  o['miniature'] = String(await page.locator('#repair-photos article > a[href^="#repair-photo-viewer-"]').count());
  const h0 = await page.evaluate(() => history.length);
  const cronologia = async () => String(await page.evaluate(() => history.length) - h0);
  const aperta = () => page.locator('.gs-viewer-frame:target');
  const stato = async chiave => {
    const n = await aperta().count();
    o[`${chiave}.hash`] = await page.evaluate(() => location.hash);
    o[`${chiave}.aperte`] = String(n);
    if (n === 1) {
      o[`${chiave}.contatore`] = testo(await aperta().locator('.gs-viewer-counter').innerText());
      o[`${chiave}.etichetta`] = await aperta().getAttribute('aria-label');
      o[`${chiave}.visibile`] = String(await aperta().isVisible());
      o[`${chiave}.foto`] = new URL(await aperta().locator('img').getAttribute('src'), base).pathname;
      o[`${chiave}.spente`] = String(await aperta().locator('.gs-viewer-off').count());
      // La foto intera del riquadro arriva davvero (le foto 1 e 2 della cattura sintetica sono byte
      // che non si decodificano: lì è false dalle due parti).
      o[`${chiave}.foto-caricata`] = String(await aperta().locator('img').evaluate(img => new Promise(fine => {
        const esito = () => fine(img.complete && img.naturalWidth > 0);
        if (img.complete) return esito();
        img.addEventListener('load', esito, {once: true});
        img.addEventListener('error', esito, {once: true});
        setTimeout(esito, 10000);
      })));
    }
    o[`${chiave}.cronologia`] = await cronologia();
  };
  const hashCambia = async (prima, azione) => {
    await azione();
    await page.waitForFunction(h => location.hash !== h, prima);
  };
  const miniatura = page.locator('#repair-photos article > a[href^="#repair-photo-viewer-"]').first();
  if (vista === 'telefono') await miniatura.tap(); else await miniatura.click();
  await aperta().waitFor();
  fa('apri');
  await stato('apri');
  let h = await page.evaluate(() => location.hash);
  await hashCambia(h, () => aperta().locator('a[data-gs-nav="next"]').click());
  await stato('barra-avanti');
  h = await page.evaluate(() => location.hash);
  await hashCambia(h, () => aperta().locator('a[data-gs-nav="prev"]').click());
  await stato('barra-indietro');
  h = await page.evaluate(() => location.hash);
  await hashCambia(h, () => page.keyboard.press('ArrowRight'));
  await stato('tasto-destra');
  h = await page.evaluate(() => location.hash);
  await hashCambia(h, () => page.keyboard.press('ArrowRight'));
  await stato('tasto-destra-2');
  // Sull'ultima la freccia a destra non fa niente.
  await page.keyboard.press('ArrowRight');
  await attendi(400);
  await stato('tasto-destra-ultima');
  h = await page.evaluate(() => location.hash);
  await hashCambia(h, () => page.keyboard.press('ArrowLeft'));
  await stato('tasto-sinistra');
  fa('dito');
  if (vista === 'telefono') {
    const cdp = await page.context().newCDPSession(page);
    const scorri = async (x0, y0, x1, y1) => {
      await cdp.send('Input.dispatchTouchEvent', {type: 'touchStart', touchPoints: [{x: x0, y: y0}]});
      await cdp.send('Input.dispatchTouchEvent', {type: 'touchMove', touchPoints: [{x: (x0 + x1) / 2, y: (y0 + y1) / 2}]});
      await cdp.send('Input.dispatchTouchEvent', {type: 'touchMove', touchPoints: [{x: x1, y: y1}]});
      await cdp.send('Input.dispatchTouchEvent', {type: 'touchEnd', touchPoints: []});
    };
    h = await page.evaluate(() => location.hash);
    await hashCambia(h, () => scorri(300, 400, 120, 410));
    await stato('dito-sinistra');
    h = await page.evaluate(() => location.hash);
    await hashCambia(h, () => scorri(120, 400, 300, 390));
    await stato('dito-destra');
    // Troppo corto (sotto i 50 px) e più verticale che orizzontale: niente.
    await scorri(200, 400, 170, 400);
    await attendi(400);
    await stato('dito-corto');
    await scorri(200, 300, 120, 600);
    await attendi(400);
    await stato('dito-verticale');
    await cdp.detach();
  }
  h = await page.evaluate(() => location.hash);
  fa('esc');
  await hashCambia(h, () => page.keyboard.press('Escape'));
  await stato('esc');
  o['esc.visibile-galleria'] = String(await page.locator('#repair-photos').isVisible());
  // Indietro dal browser: una voce sola per tutto il giro nel visualizzatore.
  await page.goBack();
  await attendi(400);
  o['indietro.percorso'] = percorso(page) + await page.evaluate(() => location.hash);
  o['indietro.aperte'] = String(await aperta().count());
}

// La finestra di data-confirm su «Riapri busta» (RIP-S-14) e «Elimina foto» (RIP-S-32): Annulla non
// cambia niente, OK fa l'azione.
async function conferme(page, {base, app, vista, dati}, o) {
  const bustaChiusa = vista === 'desktop' ? 12 : 14;
  if (!dati) {
    await bustaAssente(page, base, app, bustaChiusa, o, `busta-${bustaChiusa}`);
    return bustaAssente(page, base, app, 20, o, 'busta-20');
  }
  let finestra = NESSUNO;
  let risposta = 'annulla';
  page.on('dialog', d => {
    finestra = `${d.type()}: ${d.message()}`;
    return risposta === 'ok' ? d.accept() : d.dismiss();
  });
  fa('riapri');
  await apriScheda(page, base, app, bustaChiusa);
  o['riapri.prima.stato'] = testo(await page.locator('#repair-status').innerText());
  await page.locator('#repair_status').selectOption({label: 'In lavorazione'});
  await page.locator('#repair_reopen_reason').fill('Prova del banco 040a');
  await page.locator('#reopen-repair').click();
  await attendi(1500);
  o['riapri.annulla.finestra'] = finestra;
  o['riapri.annulla.percorso'] = percorso(page);
  o['riapri.annulla.stato'] = testo(await page.locator('#repair-status').innerText());
  o['riapri.annulla.modulo'] = String(await page.locator('#repair-reopen-form').count());
  o['riapri.annulla.motivo'] = await page.locator('#repair_reopen_reason').inputValue();
  o['riapri.annulla.messaggio'] = await messaggio(page);
  finestra = NESSUNO;
  risposta = 'ok';
  fa('riapri-ok');
  await page.locator('#reopen-repair').click();
  await page.locator(SEL_INFO).first().waitFor();
  await pronta(page, app);
  o['riapri.ok.finestra'] = finestra;
  o['riapri.ok.percorso'] = percorso(page);
  o['riapri.ok.stato'] = testo(await page.locator('#repair-status').innerText());
  o['riapri.ok.modulo'] = String(await page.locator('#repair-reopen-form').count());
  o['riapri.ok.messaggio'] = await messaggio(page);

  finestra = NESSUNO;
  risposta = 'annulla';
  fa('elimina');
  await apriScheda(page, base, app, 20);
  const n = await numeroFoto(page);
  o['elimina.prima.foto'] = await titoloFoto(page);
  const elimina = page.locator('#repair-photos [id^="remove-repair-photo-"]').first();
  const idFoto = await elimina.getAttribute('id');
  o['elimina.pulsante'] = idFoto;
  await elimina.click();
  await attendi(1500);
  o['elimina.annulla.finestra'] = finestra;
  o['elimina.annulla.foto'] = await titoloFoto(page);
  o['elimina.annulla.pulsante'] = String(await page.locator(`#${idFoto}`).count());
  o['elimina.annulla.messaggio'] = await messaggio(page);
  finestra = NESSUNO;
  risposta = 'ok';
  await page.locator(`#${idFoto}`).click();
  await fotoDiventano(page, n - 1);
  await page.locator(SEL_INFO).first().waitFor();
  o['elimina.ok.finestra'] = finestra;
  o['elimina.ok.foto'] = await titoloFoto(page);
  o['elimina.ok.pulsante'] = String(await page.locator(`#${idFoto}`).count());
  o['elimina.ok.messaggio'] = await messaggio(page);
  o['elimina.ok.vuota'] = String(await page.locator('#repair-photos', {hasText: 'Nessuna foto caricata.'}).count());
}

// Il caricamento dalla scheda (RIP-S-29): una foto parte da sola e «Foto caricata.» sparisce dopo
// 3 s (un altro «Foto caricata.» o un altro messaggio arrivati dopo restano); 3 foto; una foto oltre
// 25 MiB. Anteprima e barra di avanzamento sono osservate e riportate.
async function caricamento(page, {base, app, vista, dati}, o) {
  const busta = vista === 'desktop' ? 5 : 6;
  if (!dati) return bustaAssente(page, base, app, busta, o, `busta-${busta}`);
  const finestre = [];
  page.on('dialog', d => { finestre.push(`${d.type()}: ${d.message()}`); return d.accept(); });
  const ctx = page.context();
  const scelta = () => page.locator('#gallery-upload input[type=file]');
  // Le pagine della scheda chieste come documento, per fase: dopo un caricamento la pagina viene dalla
  // risposta dell'invio (nessuna GET in più), e i salti d'ancora del visualizzatore non la ricaricano
  // (nel Go ogni GET senza il segno della pagina è un'apertura vera, con la sua riga d'audit).
  const documenti = [];
  page.on('request', r => {
    if (r.resourceType() === 'document' && r.method() === 'GET' && /^\/repairs\/\d+$/.test(new URL(r.url()).pathname)) documenti.push(fase);
  });
  const getScheda = f => String(documenti.filter(x => x === f).length);
  await apriScheda(page, base, app, busta);
  const n0 = await numeroFoto(page);
  o['prima.foto'] = await titoloFoto(page);
  ctx.segnali.clear();

  // Una foto: parte da sola.
  fa('una');
  await scelta().setInputFiles(foto('prima-040a.png', 1));
  await fotoDiventano(page, n0 + 1);
  await page.locator(SEL_INFO, {hasText: 'Foto caricata.'}).first().waitFor();
  await pronta(page, app);
  o['una.messaggio'] = await messaggio(page);
  o['una.errori'] = testo(await page.locator('#repair-photo-upload-errors').innerText()) || NESSUNO;
  o['una.get-scheda'] = getScheda('una');
  await attendi(1100);
  fa('seconda');
  // Una seconda, 1,1 s dopo: «Foto caricata.» resta circa 3 s da quando la pagina mostra la foto
  // nuova, anche se il tempo della prima scade prima. I tempi sono quelli della pagina, registrati
  // dall'osservatore: nello standalone il messaggio non cambia (è lo stesso testo), nel Go la pagina
  // è nuova.
  const da = ctx.eventi.length;
  await scelta().setInputFiles(foto('seconda-040a.png', 2));
  const nuova = await aspettaEvento(ctx, da, 'titolo-foto', `Foto (${n0 + 2})`, 30000);
  await page.locator(SEL_INFO, {hasText: 'Foto caricata.'}).first().waitFor();
  // Solo dopo la pagina con la foto nuova: nel Go la pagina di prima può togliere il suo messaggio
  // mentre il modulo parte.
  const via = nuova && await aspettaEvento(ctx, nuova.i + 1, 'messaggio-via', 'Foto caricata.', 9000);
  o['seconda.durata'] = durata(nuova, via);
  await pronta(page, app);
  o['seconda.dopo.messaggio'] = await messaggio(page);
  o['seconda.dopo.foto'] = await titoloFoto(page);
  o['anteprima'] = String(ctx.segnali.has('anteprima'));
  o['avanzamento'] = String(ctx.segnali.has('avanzamento'));
  o['seconda.get-scheda'] = getScheda('seconda');

  fa('visore');
  // Dopo un caricamento: visualizzatore, «›», Esc e «Indietro» del browser restano nella pagina.
  const miniatura = page.locator('#repair-photos article > a[href^="#repair-photo-viewer-"]').first();
  if (vista === 'telefono') await miniatura.tap(); else await miniatura.click();
  await page.locator('.gs-viewer-frame:target').waitFor();
  let h = await page.evaluate(() => location.hash);
  await page.locator('.gs-viewer-frame:target a[data-gs-nav="next"]').click();
  await page.waitForFunction(x => location.hash !== x, h);
  h = await page.evaluate(() => location.hash);
  await page.keyboard.press('Escape');
  await page.waitForFunction(x => location.hash !== x, h);
  await attendi(500);
  await page.goBack();
  await attendi(1500);
  o['visore.get-scheda'] = getScheda('visore');
  o['visore.percorso'] = percorso(page) + await page.evaluate(() => location.hash);
  o['visore.foto'] = await titoloFoto(page);

  fa('altro');
  // Un messaggio diverso arrivato dopo «Foto caricata.» non si cancella.
  await scelta().setInputFiles(foto('terza-040a.png', 3));
  await fotoDiventano(page, n0 + 3);
  await page.locator(SEL_INFO, {hasText: 'Foto caricata.'}).first().waitFor();
  await pronta(page, app);
  const daAltro = ctx.eventi.length;
  await page.locator('#repair-photos [id^="remove-repair-photo-"]').first().click();
  await fotoDiventano(page, n0 + 2);
  const altro = await aspettaEvento(ctx, daAltro, 'messaggio-compare', 'Foto eliminata.', 30000);
  await pronta(page, app);
  // Quattro secondi dopo che è comparso, il messaggio diverso c'è ancora.
  await attendi(Math.max(0, altro.t + 4000 - ADESSO()));
  o['altro.dopo-4s.messaggio'] = await messaggio(page);
  o['altro.sparito'] = String(ctx.eventi.slice(daAltro).some(e => e.cosa === 'messaggio-via' && e.valore === 'Foto eliminata.'));

  // Cinque foto insieme: più del massimo dalle due parti (2 nello standalone, 4 nel Go: D-59).
  fa('cinque');
  await apriScheda(page, base, app, busta);
  const n3 = await numeroFoto(page);
  page.context().segnali.clear();
  await scelta().setInputFiles([foto('a-040a.png', 4), foto('b-040a.png', 5), foto('c-040a.png', 6), foto('d-044b.png', 17),
    foto('e-044b.png', 18)]);
  await page.waitForFunction(n => {
    const e = document.querySelector('#repair-photo-upload-errors');
    const h = document.querySelector('#repair-photos h2');
    return (e && e.textContent.trim() !== '') || (h && !h.textContent.includes(`(${n})`));
  }, n3, {timeout: 30000});
  await attendi(6000);
  await pronta(page, app);
  o['cinque.foto-in-piu'] = String(await numeroFoto(page) - n3);
  o['cinque.errori-visti'] = visti(page, 'errori-foto');
  o['cinque.messaggi-visti'] = visti(page, 'messaggio');
  o['cinque.errori-finali'] = testo(await page.locator('#repair-photo-upload-errors').innerText()) || NESSUNO;

  // Una foto oltre 25 MiB.
  fa('grande');
  await apriScheda(page, base, app, busta);
  const ng = await numeroFoto(page);
  page.context().segnali.clear();
  await scelta().setInputFiles(fotoGrande());
  await page.waitForFunction(() => {
    const e = document.querySelector('#repair-photo-upload-errors');
    return e && e.textContent.trim() !== '';
  }, null, {timeout: 60000});
  await attendi(1500);
  await pronta(page, app);
  o['grande.errori'] = testo(await page.locator('#repair-photo-upload-errors').innerText()) || NESSUNO;
  o['grande.foto-in-piu'] = String(await numeroFoto(page) - ng);
  o['grande.messaggio'] = await messaggio(page);
  o['grande.errore'] = await errore(page);
  o['grande.errori-visti'] = visti(page, 'errori-foto');
  o['grande.messaggi-visti'] = visti(page, 'messaggio');
  o['finestre'] = finestre.join(' | ') || NESSUNO;
}

// La coda delle foto di «Nuova riparazione» (RIP-H-40, slice 040b): foto scelte prima di generare la
// busta (formato sbagliato, tre insieme, «Rimuovi»), salvate e mandate una alla volta mentre si
// compila la busta dopo, con la barra; un errore definitivo (una foto che il server rifiuta, 422) e
// uno da riprovare (la connessione che cade, «Riprova»); la conferma prima di ricaricare con foto in
// coda; foto scelte e poi lasciate (si esce e si torna): non vanno a un'altra busta; la prima foto è
// la foto della busta (mai all'artigiano). Le richieste della coda passano da una rotta della pagina
// che le trattiene finché lo scenario non le lascia andare, o ne interrompe una: le stesse mosse
// sulle due app. I numeri delle buste nuove diventano B1, B2, le percentuali N%.
async function coda(page, {base, app, vista, dati}, o) {
  if (!dati) return bustaAssente(page, base, app, 1, o, 'busta-1');
  const ctx = page.context();
  const finestre = [];
  page.on('dialog', d => {
    finestre.push(d.type() === 'beforeunload' ? 'beforeunload' : `${d.type()}: ${d.message()}`);
    return d.dismiss();
  });
  let modo = 'trattieni';
  const trattenute = [];
  let fotoInviate = 0;
  // L'avviso della busta salvata manomesso (manomesso = true): la chiave del modulo cambiata nella
  // pagina dell'esito dopo il salvataggio (Go) o nell'evento intake-photos-saved del websocket
  // (standalone).
  let manomesso = false;
  const CHIAVE_FALSA = 'chiave-manomessa-040b';
  await page.routeWebSocket('**/*', ws => {
    const u = new URL(ws.url());
    if (!(u.protocol === 'wss:' && `https://${u.host}` === base)) { ctx.guasti.fuori++; return ws.close(); }
    const server = ws.connectToServer();
    server.onMessage(m => ws.send(manomesso && typeof m === 'string'
      ? m.replace(/"intake_key":"[^"]*"/g, `"intake_key":"${CHIAVE_FALSA}"`) : m));
    ws.onMessage(m => server.send(m));
  });
  ctx.trasforma = req => (manomesso && req.method() === 'POST' && new URL(req.url()).pathname === '/nuova'
    ? corpo => corpo.replace(/data-intake-key="[^"]*"/g, `data-intake-key="${CHIAVE_FALSA}"`)
    : null);
  await page.route(u => /^\/repairs\/\d+\/photos$/.test(u.pathname), async route => {
    if (route.request().method() !== 'POST') return route.fallback();
    fotoInviate++;
    // Dopo la connessione interrotta i tentativi da soli del Go (D-58) restano trattenuti finché lo
    // scenario non preme «Riprova»: così la striscia letta non cambia sotto.
    if (modo === 'interrompi') { modo = 'trattieni'; return route.abort('failed'); }
    if (modo === 'trattieni') { trattenute.push(route); return; }
    return route.fallback();
  });
  // Una richiesta trattenuta di una pagina che non c'è più (il Go ricarica, D-58) può non partire più.
  const lascia = async () => { modo = 'passa'; for (const r of trattenute.splice(0)) await r.fallback().catch(() => {}); };
  const numeri = [];
  const anonimo = t => numeri.reduce((x, n, i) => x.split(`Busta ${n}:`).join(`Busta B${i + 1}:`)
    .split(`Riparazione ${n} creata.`).join(`Riparazione B${i + 1} creata.`)
    .split(`Busta ${n} salvata`).join(`Busta B${i + 1} salvata`), t).replace(/\d+%/g, 'N%');
  const righe = () => page.locator('#photo-queue > div');
  const striscia = async () => {
    const n = await righe().count();
    if (n === 0 || !(await page.locator('#photo-queue').isVisible())) return NESSUNO;
    const parti = [];
    for (let i = 0; i < n; i++) parti.push(testo(await righe().nth(i).innerText()));
    return anonimo(parti.join(' | '));
  };
  const sezione = async chiave => {
    o[`${chiave}.stato`] = await leggi(page, '#intake-photo-status > p');
    o[`${chiave}.anteprime`] = (await page.locator('#intake-photo-status img').evaluateAll(l =>
      l.map(i => `${i.alt} (${i.src.startsWith('blob:') ? 'blob' : 'altro'})`))).join(' | ') || NESSUNO;
    o[`${chiave}.errori`] = testo(await page.locator('#intake-photo-errors').innerText()) || NESSUNO;
    o[`${chiave}.conteggio`] = await page.locator('input[name="intake_photos"]').inputValue();
  };
  const galleria = () => page.locator('#intake-gallery-input');
  const fotocamera = () => page.locator('#intake-camera-input');
  const nuovaPagina = async () => {
    await page.locator('#intake-photos').waitFor({state: 'visible'});
    await pronta(page, app);
  };
  const compila = async n => {
    await page.locator('[name="repair[customer_name]"]').fill(`Cliente coda 040b ${n}`);
    await page.locator('[name="repair[description]"]').fill(`Anello della coda ${n}`);
    await page.locator('[name="repair[qr_code]"]').fill(`QR-040B-${vista}-${n}`);
  };
  const genera = async () => {
    await page.locator('#new-repair-form button[type=submit]').click();
    await page.locator(SEL_INFO, {hasText: SALVATA}).first().waitFor();
    const m = (await messaggio(page)).match(NUMERO_SALVATA);
    if (m) numeri.push(m[1] || m[2]);
    await pronta(page, app);
  };
  const nonFoto = nome => ({name: nome, mimeType: 'image/png', buffer: Buffer.from('non una foto')});

  fa('apri');
  await page.goto(`${base}/nuova`);
  await nuovaPagina();
  o['apri.sezione'] = String(await page.locator('#intake-photos').isVisible());
  o['apri.pulsanti'] = (await page.locator('#intake-camera-control, #intake-gallery-control').allInnerTexts()).map(testo).join(' | ');
  o['apri.striscia'] = await striscia();
  await sezione('apri');

  fa('formato');
  await galleria().setInputFiles({name: 'nota-040b.txt', mimeType: 'text/plain', buffer: Buffer.from('nota')});
  await sezione('formato');
  fa('cinque');
  await galleria().setInputFiles([foto('a-040b.png', 11), foto('b-040b.png', 12), foto('c-040b.png', 13), foto('d-044b.png', 19),
    foto('e-044b.png', 20)]);
  await sezione('cinque');

  fa('scelte');
  await galleria().setInputFiles([foto('uno-040b.png', 7), nonFoto('rotta-040b.png')]);
  await fotocamera().setInputFiles([foto('due-040b.png', 8), foto('extra-040b.png', 9)]);
  await sezione('scelte-quattro');
  await page.locator('#intake-photo-status button[aria-label="Rimuovi extra-040b.png"]').click();
  await sezione('scelte');

  fa('salva-1');
  await compila(1);
  await genera();
  await righe().first().waitFor();
  o['salva-1.percorso'] = percorso(page);
  o['salva-1.messaggio'] = anonimo(await messaggio(page));
  o['salva-1.striscia'] = await striscia();
  o['salva-1.barre'] = String(await page.locator('#photo-queue progress').count());
  const id1 = await righe().first().getAttribute('data-repair-id');

  fa('esci');
  // Ricaricare con delle foto in coda chiede prima; «Annulla» lascia la pagina e la coda come sono.
  // Nel Go (D-58) non chiede: la pagina si ricarica e la coda riprende.
  await page.evaluate(() => { setTimeout(() => window.location.reload(), 0); });
  await attendi(1500);
  o['esci.finestre'] = finestre.join(' | ') || NESSUNO;
  o['esci.percorso'] = percorso(page);
  o['esci.striscia'] = await striscia();

  fa('seconda');
  await page.locator('#new-repair-link').click();
  await nuovaPagina();
  o['seconda.percorso'] = percorso(page);
  o['seconda.striscia-prima'] = await striscia();
  await sezione('seconda');
  // Le foto della prima busta salgono mentre si compila la seconda.
  await lascia();
  await compila(2);
  await page.locator('#photo-queue > div[role="alert"]').first().waitFor({timeout: 60000});
  o['seconda.striscia-dopo'] = await striscia();
  o['seconda.percorso-dopo'] = percorso(page);
  await galleria().setInputFiles(foto('tre-040b.png', 10));
  await sezione('seconda-scelta');

  fa('salva-2');
  modo = 'interrompi';
  await genera();
  // Letta nella pagina nello stesso istante in cui compare «Riprova».
  const conRiprova = await page.waitForFunction(() => {
    const r = [...document.querySelectorAll('#photo-queue > div')];
    if (!r.some(x => x.textContent.includes('Riprova'))) return null;
    return r.map(x => x.innerText.replace(/\s+/g, ' ').trim()).join(' | ');
  }, null, {timeout: 60000});
  // Il conto alla rovescia del Go accanto a «Riprova» (slice 055d, D-58): i secondi dipendono da quando
  // si legge, il testo no. Solo il numero diventa N; la differenza dallo standalone resta e si dichiara.
  o['salva-2.striscia'] = anonimo(await conRiprova.jsonValue()).replace(/nuovo tentativo tra \d+ s…/g, 'nuovo tentativo tra N s…');
  const id2 = await righe().nth(1).getAttribute('data-repair-id');
  fa('riprova');
  await page.locator('#photo-queue button', {hasText: 'Riprova'}).click();
  await lascia();
  await page.waitForFunction(() => document.querySelectorAll('#photo-queue > div').length === 1, null, {timeout: 60000});
  o['riprova.striscia'] = await striscia();
  fa('chiudi');
  await page.locator('#photo-queue button[aria-label^="Lascia perdere"]').click();
  await attendi(300);
  o['chiudi.striscia'] = await striscia();
  o['chiudi.nascosta'] = String(await page.locator('#photo-queue').isHidden());

  fa('chiave');
  // Foto scelte su un modulo lasciato: tornando a «Nuova riparazione» il modulo è un altro e le foto
  // non sono più sue.
  await page.locator('#new-repair-link').click();
  await nuovaPagina();
  await galleria().setInputFiles(foto('lasciata-040b.png', 14));
  await sezione('chiave-scelta');
  await page.locator('#new-repair-back').click();
  await page.locator('#new-repair-link').waitFor();
  await pronta(page, app);
  o['chiave.lista'] = percorso(page);
  await page.locator('#new-repair-link').click();
  await nuovaPagina();
  await sezione('chiave');
  await compila(3);
  await genera();
  await attendi(2000);
  o['chiave.striscia'] = await striscia();
  o['chiave.messaggio'] = anonimo(await messaggio(page));

  fa('rifiuto');
  // Con delle foto scelte: prima l'avviso del QR, poi un salvataggio rifiutato (nome vuoto). Nessuna
  // foto parte e restano scelte; il salvataggio giusto le manda alla sua busta, solo dopo.
  await page.locator('#new-repair-link').click();
  await nuovaPagina();
  await galleria().setInputFiles(foto('giusta-040b.png', 15));
  await page.locator('[name="repair[customer_name]"]').fill('Cliente coda 040b 4');
  await page.locator('[name="repair[description]"]').fill('Anello della coda 4');
  const inviatePrima = fotoInviate;
  await page.locator('#new-repair-form button[type=submit]').click();
  await page.locator('#intake-qr-warning').waitFor();
  await pronta(page, app);
  // Nel Go la sezione si mostra quando ha ritrovato le foto scelte nel browser (D-58).
  await page.locator('#intake-photos').waitFor({state: 'visible'});
  await sezione('rifiuto-qr');
  o['rifiuto-qr.pulsante'] = testo(await page.locator('#new-repair-form button[type=submit]').innerText());
  await page.locator('[name="repair[customer_name]"]').fill('');
  await page.locator('[name="repair[qr_code]"]').fill(`QR-040B-${vista}-4`);
  await page.locator('#new-repair-form button[type=submit]').click();
  await page.locator(SEL_ERRORE).first().waitFor();
  await pronta(page, app);
  await page.locator('#intake-photos').waitFor({state: 'visible'});
  o['rifiuto-nome.errore'] = await errore(page);
  o['rifiuto-nome.percorso'] = percorso(page);
  await sezione('rifiuto-nome');
  o['rifiuto-nome.foto-mandate'] = String(fotoInviate - inviatePrima);
  o['rifiuto-nome.striscia'] = await striscia();
  modo = 'trattieni';
  await page.locator('[name="repair[customer_name]"]').fill('Cliente coda 040b 4');
  await genera();
  await righe().first().waitFor();
  o['rifiuto.striscia'] = await striscia();
  const id4 = await righe().first().getAttribute('data-repair-id');
  await lascia();
  await page.waitForFunction(() => document.querySelectorAll('#photo-queue > div').length === 0, null, {timeout: 60000});
  o['rifiuto.foto-mandate'] = String(fotoInviate - inviatePrima);

  fa('manomesso');
  await page.locator('#new-repair-link').click();
  await nuovaPagina();
  await galleria().setInputFiles(foto('manomessa-040b.png', 16));
  await compila(5);
  const inviateManomesso = fotoInviate;
  manomesso = true;
  await genera();
  await attendi(2000);
  manomesso = false;
  o['manomesso.striscia'] = await striscia();
  o['manomesso.foto-mandate'] = String(fotoInviate - inviateManomesso);
  await page.locator('#new-repair-link').click();
  await nuovaPagina();
  await sezione('manomesso-dopo');

  fa('schede');
  const canali = async () => (await page.locator('#repair-photos [id^="artisan-photo-"]').allInnerTexts()).map(testo).join(' | ') || NESSUNO;
  await apriScheda(page, base, app, id1);
  o['scheda-1.foto'] = await titoloFoto(page);
  o['scheda-1.artigiano'] = await canali();
  await apriScheda(page, base, app, id2);
  o['scheda-2.foto'] = await titoloFoto(page);
  o['scheda-2.artigiano'] = await canali();
  await apriScheda(page, base, app, id4);
  o['scheda-4.foto'] = await titoloFoto(page);
  o['scheda-4.artigiano'] = await canali();
  o['numeri'] = String(numeri.length);
  // La foto rotta: l'unica risposta d'errore attesa (422 della coda), contata qui e tolta dai guasti.
  const rifiutate = ctx.guasti.risposte.filter(r => /^422 POST \/repairs\/\d+\/photos$/.test(r));
  ctx.guasti.risposte = ctx.guasti.risposte.filter(r => !/^422 POST \/repairs\/\d+\/photos$/.test(r));
  o['rifiutate-dal-server'] = String(rifiutate.length);
  // La connessione interrotta dalla rotta: l'unico errore della console atteso, contato e tolto.
  const caduta = 'console: Failed to load resource: net::ERR_FAILED';
  o['connessioni-cadute'] = String(ctx.guasti.js.filter(e => e === caduta).length);
  ctx.guasti.js = ctx.guasti.js.filter(e => e !== caduta);
  o['finestre'] = finestre.join(' | ') || NESSUNO;
}

// La coda delle foto in background (slice 044b, D-58, D-59): dopo «Genera busta» si lavora altrove e
// le foto salgono lo stesso. Il massimo di 4 foto per fonte (5 no, 4 sì); una busta con 4 foto e
// subito un'altra scheda e una pagina del menu, poi la lista: le foto sono salite, una volta; una
// ricarica a metà invio, con la prima foto già salvata dal server e la risposta che non arriva alla
// pagina: dopo la ricarica salgono le altre, senza doppioni; due buste di fila con le pagine che
// cambiano: ogni foto alla sua busta; un altro utente sullo stesso browser non manda le foto del
// primo, che salgono quando il primo rientra. Le finestre (beforeunload dello standalone) si
// accettano: nel Go non ce ne sono. Le richieste della coda passano da una rotta che le conta, le
// trattiene finché lo scenario non le lascia, o le fa arrivare al server ricaricando la pagina
// prima della risposta. Lo standalone perde la coda a ogni pagina caricata da capo: i campi che lo
// mostrano sono solo del Go (solo_go) o deviazioni D-58.
async function sfondo(page, {base, app, vista, dati, credDipendente, cred}, o) {
  if (!dati) return bustaAssente(page, base, app, 1, o, 'busta-1');
  const ctx = page.context();
  const finestre = [];
  page.on('dialog', d => {
    finestre.push(d.type() === 'beforeunload' ? 'beforeunload' : `${d.type()}: ${d.message()}`);
    return d.accept();
  });
  let modo = 'passa';
  const trattenute = [];
  await page.route(u => /^\/repairs\/\d+\/photos$/.test(u.pathname), async route => {
    if (route.request().method() !== 'POST') return route.fallback();
    if (modo === 'trattieni') { trattenute.push(route); return; }
    return route.fallback();
  });
  // ricarica fa arrivare al server la richiesta trattenuta, che salva la foto, e ricarica la pagina
  // prima che la risposta le arrivi.
  const ricarica = async route => {
    const r = {busta: new URL(route.request().url()).pathname.split('/')[2], codice: 'errore'};
    try { r.codice = String((await route.fetch({maxRedirects: 0, timeout: 90000})).status()); } catch { /* resta errore */ }
    await page.evaluate(() => { setTimeout(() => window.location.reload(), 0); }).catch(() => {});
    await route.abort('failed').catch(() => {});
    return r;
  };
  // mandaECambia fa lo stesso con un altro cambio di pagina (cambia), e dà il codice del server.
  const mandaECambia = async (route, cambia) => {
    let codice = 'errore';
    try { codice = String((await route.fetch({maxRedirects: 0, timeout: 90000})).status()); } catch { /* resta errore */ }
    await cambia();
    await route.abort('failed').catch(() => {});
    return codice;
  };
  const lascia = async () => { modo = 'passa'; for (const r of trattenute.splice(0)) await r.fallback().catch(() => {}); };
  // Le richieste trattenute di una pagina lasciata (l'utente esce) non arrivano più al server.
  const ferma = async () => { modo = 'passa'; for (const r of trattenute.splice(0)) await r.abort('failed').catch(() => {}); };
  const numeri = [];
  const anonimo = t => numeri.reduce((x, n, i) => x.split(`Busta ${n}:`).join(`Busta B${i + 1}:`)
    .split(`Riparazione ${n} creata.`).join(`Riparazione B${i + 1} creata.`)
    .split(`Busta ${n} salvata`).join(`Busta B${i + 1} salvata`), t).replace(/\d+%/g, 'N%');
  const striscia = async () => {
    try {
      const r = page.locator('#photo-queue > div');
      const n = await r.count();
      if (n === 0 || !(await page.locator('#photo-queue').isVisible())) return NESSUNO;
      const parti = [];
      for (let i = 0; i < n; i++) parti.push(testo(await r.nth(i).innerText()));
      return anonimo(parti.join(' | '));
    } catch {
      return 'pagina che cambia';
    }
  };
  // vuota aspetta (al più ms, senza fermare lo scenario) che la striscia sia vuota, anche mentre le
  // pagine cambiano; dà quello che mostra alla fine.
  const vuota = async ms => {
    const fine = ADESSO() + ms;
    for (;;) {
      const v = await striscia();
      if (v === NESSUNO || ADESSO() > fine) return v;
      await attendi(250);
    }
  };
  const galleria = () => page.locator('#intake-gallery-input');
  const fotocamera = () => page.locator('#intake-camera-input');
  const nuovaPagina = async () => {
    await page.locator('#intake-photos').waitFor({state: 'visible'});
    await pronta(page, app);
  };
  const sezione = async chiave => {
    o[`${chiave}.stato`] = await leggi(page, '#intake-photo-status > p');
    o[`${chiave}.errori`] = testo(await page.locator('#intake-photo-errors').innerText()) || NESSUNO;
    o[`${chiave}.conteggio`] = await page.locator('input[name="intake_photos"]').inputValue();
  };
  const compila = async n => {
    await page.locator('[name="repair[customer_name]"]').fill(`Cliente sfondo 044b ${n}`);
    await page.locator('[name="repair[description]"]').fill(`Collana dello sfondo ${n}`);
    await page.locator('[name="repair[qr_code]"]').fill(`QR-044B-${vista}-${n}`);
  };
  const genera = async () => {
    await page.locator('#new-repair-form button[type=submit]').click();
    await page.locator(SEL_INFO, {hasText: SALVATA}).first().waitFor();
    const m = (await messaggio(page)).match(NUMERO_SALVATA);
    if (m) numeri.push(m[1] || m[2]);
    await pronta(page, app);
    return m ? m[1] || m[2] : '';
  };
  // idDella è l'id della busta numero n, dalla sua riga nella striscia (con le foto trattenute).
  const idDella = async n => page.locator('#photo-queue > div', {hasText: `Busta ${n}:`}).first().getAttribute('data-repair-id');
  const nuova = async () => {
    await page.goto(`${base}/`);
    await pronta(page, app);
    await page.locator('#new-repair-link').click();
    await nuovaPagina();
  };
  // menu apre ☰ e sceglie la voce. «Esci» si preme senza guardare cosa c'è sopra: sul telefono la
  // striscia della coda dello standalone (z-index 50) copre il fondo del menu aperto.
  const menu = async voce => {
    await page.locator('[id$="-mobile-menu-button"]').first().click();
    const v = page.locator('[id$="-mobile-menu"]').first().getByText(voce, {exact: true});
    if (voce === 'Esci') await v.dispatchEvent('click'); else await v.click();
  };
  const fotoDella = async id => {
    await apriScheda(page, base, app, id);
    return titoloFoto(page);
  };

  fa('limite');
  await page.goto(`${base}/nuova`);
  await nuovaPagina();
  await galleria().setInputFiles([1, 2, 3, 4, 5].map(i => foto(`limite-${i}-044b.png`, 30 + i)));
  await sezione('cinque');
  await galleria().setInputFiles([1, 2, 3, 4].map(i => foto(`quattro-${i}-044b.png`, 40 + i)));
  await sezione('quattro');
  // Via le foto scelte (nel Go le quattro; nello standalone nessuna), per ripartire uguali.
  while (await page.locator('#intake-photo-status button[aria-label^="Rimuovi"]').count() > 0) {
    await page.locator('#intake-photo-status button[aria-label^="Rimuovi"]').first().click();
  }
  await sezione('limite-tolte');

  fa('naviga');
  // Quattro foto (2 dalla galleria e 2 dalla fotocamera, il massimo per fonte dello standalone), la
  // busta salvata, e subito un'altra scheda, il menu e la lista.
  await galleria().setInputFiles([foto('naviga-1-044b.png', 51), foto('naviga-2-044b.png', 52)]);
  await fotocamera().setInputFiles([foto('naviga-3-044b.png', 53), foto('naviga-4-044b.png', 54)]);
  await sezione('naviga-scelte');
  await compila(1);
  modo = 'trattieni';
  const n1 = await genera();
  const id1 = await idDella(n1);
  o['naviga.striscia'] = await striscia();
  // La prima foto arriva al server, che la salva; la pagina cambia (un'altra scheda) prima della
  // risposta: un cambio di pagina a metà invio, deterministico come la ricarica sotto.
  const daNaviga = ADESSO();
  while (trattenute.length === 0 && ADESSO() - daNaviga < 30000) await attendi(100);
  const primaNaviga = trattenute.shift();
  modo = 'passa';
  await lascia();
  o['naviga.server'] = primaNaviga ? await mandaECambia(primaNaviga, () => apriScheda(page, base, app, 1)) : 'nessun invio';
  o['naviga.scheda'] = percorso(page);
  fa('naviga-menu');
  await menu('Registro attività');
  await page.waitForURL(`${base}/audit`);
  await pronta(page, app);
  o['naviga.menu'] = percorso(page);
  fa('naviga-lista');
  await page.locator('#fab-home').click();
  await page.waitForURL(`${base}/`);
  await pronta(page, app);
  o['naviga.lista'] = percorso(page);
  o['naviga.fine'] = await vuota(60000);
  o['naviga.foto'] = await fotoDella(id1);

  fa('ricarica');
  // Due foto; la prima arriva al server, che la salva, ma la pagina si ricarica prima della risposta.
  await nuova();
  await galleria().setInputFiles([foto('ricarica-1-044b.png', 61), foto('ricarica-2-044b.png', 62)]);
  await compila(2);
  modo = 'trattieni';
  await genera();
  const daRicarica = ADESSO();
  while (trattenute.length === 0 && ADESSO() - daRicarica < 30000) await attendi(100);
  const prima = trattenute.shift();
  modo = 'passa';
  await lascia();
  const ricaricata = prima ? await ricarica(prima) : null;
  await attendi(1500);
  await pronta(page, app).catch(() => {});
  o['ricarica.server'] = ricaricata ? ricaricata.codice : 'nessun invio';
  o['ricarica.fine'] = await vuota(60000);
  o['ricarica.foto'] = ricaricata ? await fotoDella(ricaricata.busta) : 'nessun invio';
  // La risposta interrotta dalla rotta: l'unico errore della console atteso, tolto (nel Go la pagina
  // se ne va prima e di solito non lo scrive).
  ctx.guasti.js = ctx.guasti.js.filter(e => e !== 'console: Failed to load resource: net::ERR_FAILED');

  fa('due-buste');
  // Due buste di fila: la seconda si compila mentre salgono le foto della prima, poi si va a una
  // scheda e alla lista.
  await nuova();
  await galleria().setInputFiles(foto('prima-busta-044b.png', 71));
  await compila(3);
  modo = 'trattieni';
  const n3 = await genera();
  const id3 = await idDella(n3);
  await lascia();
  await page.locator('#new-repair-link').click();
  await nuovaPagina();
  await fotocamera().setInputFiles([foto('seconda-busta-1-044b.png', 72), foto('seconda-busta-2-044b.png', 73)]);
  await compila(4);
  modo = 'trattieni';
  const n4 = await genera();
  const id4 = await idDella(n4);
  await lascia();
  await apriScheda(page, base, app, 1);
  await page.locator('#fab-home').click();
  await page.waitForURL(`${base}/`);
  await pronta(page, app);
  o['due-buste.fine'] = await vuota(60000);
  o['due-buste.foto-prima'] = await fotoDella(id3);
  o['due-buste.foto-seconda'] = await fotoDella(id4);

  fa('altro-utente');
  // Il titolare salva una busta con una foto ed esce prima che salga; il dipendente entra sullo stesso
  // browser e la foto non parte; il titolare rientra e la foto sale.
  await nuova();
  await galleria().setInputFiles(foto('altro-utente-044b.png', 81));
  await compila(5);
  modo = 'trattieni';
  const n5 = await genera();
  const id5 = await idDella(n5);
  fa('altro-utente-esci');
  await menu('Esci');
  await page.waitForURL(`${base}/login`);
  await ferma();
  // Le foto mandate da qualunque pagina del contesto (anche quella dell'accesso) mentre c'è il
  // dipendente.
  const primaDip = ctx.inviiFoto;
  await accedi(ctx, base, app, credDipendente);
  await page.goto(`${base}/`);
  await pronta(page, app);
  await attendi(5000);
  o['altro-utente.dipendente-striscia'] = await striscia();
  o['altro-utente.dipendente-inviate'] = String(ctx.inviiFoto - primaDip);
  fa('altro-utente-rientra');
  await menu('Esci');
  await page.waitForURL(`${base}/login`);
  await accedi(ctx, base, app, cred);
  await page.goto(`${base}/`);
  await pronta(page, app);
  o['altro-utente.titolare-fine'] = await vuota(60000);
  o['altro-utente.foto'] = await fotoDella(id5);

  fa('scadenza');
  // Voci vecchie nel browser: una foto scelta 25 ore fa e mai salvata, e una foto in coda da 15 giorni
  // (busta 1); alla pagina dopo il Go le toglie senza mandarle (24 ore e 14 giorni). Lo standalone
  // non ha la base: le voci restano (solo_go).
  await page.goto(`${base}/`);
  await pronta(page, app);
  const scriviVecchie = () => page.evaluate(async () => {
    const utente = (document.querySelector("meta[name='gestrip-utente']") || {}).content || '1';
    const ora = Date.now();
    const db = await new Promise((fine, errore) => {
      const r = indexedDB.open('gestrip-coda-foto', 1);
      r.onupgradeneeded = () => { r.result.createObjectStore('voci', {keyPath: 'id'}); r.result.createObjectStore('file'); };
      r.onsuccess = () => fine(r.result);
      r.onerror = () => errore(r.error);
    });
    await new Promise((fine, errore) => {
      const tx = db.transaction(['voci', 'file'], 'readwrite');
      tx.objectStore('voci').put({id: 'vecchiasceltaAAAAAAAAA', user: utente, tab: 'altra', key: 'chiave-vecchia', state: 'scelta',
        source: 'gallery', name: 'vecchia.png', type: 'image/png', created: ora - 25 * 3600 * 1000, order: 0});
      tx.objectStore('voci').put({id: 'vecchiacodaAAAAAAAAAAA', user: utente, tab: 'altra', key: 'chiave-coda', state: 'attesa',
        source: 'gallery', name: 'vecchia-coda.png', type: 'image/png', created: ora - 15 * 86400 * 1000, order: 0, repairId: '1',
        bag: 'vecchia', job: 'chiave-coda', total: 1, position: 0, jobCreated: ora - 15 * 86400 * 1000, tries: 0, retryAt: 0, error: ''});
      tx.objectStore('file').put(new Blob(['non una foto'], {type: 'image/png'}), 'vecchiacodaAAAAAAAAAAA');
      tx.oncomplete = fine;
      tx.onerror = () => errore(tx.error);
    });
    db.close();
  });
  const contaVecchie = () => page.evaluate(async () => {
    const db = await new Promise(fine => { const r = indexedDB.open('gestrip-coda-foto', 1); r.onsuccess = () => fine(r.result); });
    const n = await new Promise(fine => {
      const r = db.transaction('voci').objectStore('voci').getAll();
      r.onsuccess = () => fine(r.result.filter(x => x.id.startsWith('vecchia')).length);
    });
    db.close();
    return n;
  });
  const primaVecchie = ctx.inviiFoto;
  await scriviVecchie();
  await page.goto(`${base}/nuova`);
  await pronta(page, app);
  await page.locator('#intake-photos').waitFor({state: 'visible'});
  await attendi(1000);
  o['scadenza.voci'] = String(await contaVecchie());
  o['scadenza.inviate'] = String(ctx.inviiFoto - primaVecchie);
  o['scadenza.errori'] = testo(await page.locator('#intake-photo-errors').innerText()) || NESSUNO;
  o['scadenza.striscia'] = await striscia();

  o['numeri'] = String(numeri.length);
  o['finestre'] = finestre.join(' | ') || NESSUNO;
}

// Più schede dello stesso browser (slice 044b, D-58). Una scheda duplicata (lo stesso sessionStorage,
// copiato in una pagina nuova prima che parta, come «Duplica scheda») che apre «Nuova riparazione»
// non toglie le foto scelte nella prima, e la prima le manda alla sua busta. Con due schede aperte
// sulle pagine interne una sola manda: ogni foto arriva alla rete una volta (la prima, trattenuta,
// tiene il lucchetto).
async function schede(page, {base, app, vista, dati}, o) {
  if (!dati) return bustaAssente(page, base, app, 1, o, 'busta-1');
  const ctx = page.context();
  page.on('dialog', d => d.accept());
  let modo = 'passa';
  const trattenute = [];
  await page.route(u => /^\/repairs\/\d+\/photos$/.test(u.pathname), async route => {
    if (route.request().method() !== 'POST') return route.fallback();
    if (modo === 'trattieni') { trattenute.push(route); return; }
    return route.fallback();
  });
  const lascia = async () => { modo = 'passa'; for (const r of trattenute.splice(0)) await r.fallback().catch(() => {}); };
  const sezione = async (p, chiave) => {
    o[`${chiave}.stato`] = await leggi(p, '#intake-photo-status > p');
    o[`${chiave}.errori`] = testo(await p.locator('#intake-photo-errors').innerText()) || NESSUNO;
  };
  const compila = async n => {
    await page.locator('[name="repair[customer_name]"]').fill(`Cliente schede 044b ${n}`);
    await page.locator('[name="repair[description]"]').fill(`Spilla delle schede ${n}`);
    await page.locator('[name="repair[qr_code]"]').fill(`QR-044B-SCHEDE-${vista}-${n}`);
  };
  const genera = async () => {
    await page.locator('#new-repair-form button[type=submit]').click();
    await page.locator(SEL_INFO, {hasText: SALVATA}).first().waitFor();
    await pronta(page, app);
  };
  const vuota = async ms => {
    const fine = ADESSO() + ms;
    while (ADESSO() < fine) {
      if (await page.locator('#photo-queue > div').count().catch(() => 1) === 0) return NESSUNO;
      await attendi(250);
    }
    return 'non vuota';
  };
  const idDellaPrima = async () => page.locator('#photo-queue > div').first().getAttribute('data-repair-id');

  fa('duplicata');
  await page.goto(`${base}/nuova`);
  await page.locator('#intake-photos').waitFor({state: 'visible'});
  await pronta(page, app);
  await page.locator('#intake-gallery-input').setInputFiles([foto('dup-1-044b.png', 91), foto('dup-2-044b.png', 92)]);
  await attendi(500);
  const copia = await page.evaluate(() => JSON.stringify(Object.entries(sessionStorage)));
  const seconda = await ctx.newPage();
  await seconda.addInitScript(voci => {
    if (sessionStorage.getItem('__banco-copiata')) return;
    for (const [k, v] of JSON.parse(voci)) sessionStorage.setItem(k, v);
    sessionStorage.setItem('__banco-copiata', '1');
  }, copia);
  await seconda.goto(`${base}/nuova`);
  await seconda.locator('#intake-photos').waitFor({state: 'visible'});
  await pronta(seconda, app);
  await attendi(1000);
  await sezione(seconda, 'duplicata.seconda');
  await seconda.close();
  await sezione(page, 'duplicata.prima');
  await compila(1);
  modo = 'trattieni';
  await genera();
  await page.locator('#photo-queue > div').first().waitFor();
  const id1 = await idDellaPrima();
  await lascia();
  o['duplicata.fine'] = await vuota(60000);
  await apriScheda(page, base, app, id1);
  o['duplicata.foto'] = await titoloFoto(page);

  fa('due-schede');
  await page.goto(`${base}/nuova`);
  await page.locator('#intake-photos').waitFor({state: 'visible'});
  await pronta(page, app);
  await page.locator('#intake-gallery-input').setInputFiles([foto('due-1-044b.png', 93), foto('due-2-044b.png', 94)]);
  await compila(2);
  modo = 'trattieni';
  await genera();
  const daDue = ADESSO();
  while (trattenute.length === 0 && ADESSO() - daDue < 30000) await attendi(100);
  const id2 = await idDellaPrima();
  const prima = ctx.idInvii.length;
  // Una seconda scheda su una pagina interna mentre la prima manda (la sua richiesta è trattenuta).
  const altra = await ctx.newPage();
  await altra.goto(`${base}/`);
  await pronta(altra, app);
  await attendi(3000);
  await lascia();
  o['due-schede.fine'] = await vuota(60000);
  await attendi(1000);
  await altra.close();
  // Lo standalone non manda l'identificativo: lì i doppi non si contano.
  const ids = ctx.idInvii.slice(prima).filter(Boolean);
  o['due-schede.invii'] = String(ctx.idInvii.length - prima);
  o['due-schede.doppi'] = String(ids.length - new Set(ids).size);
  await apriScheda(page, base, app, id2);
  o['due-schede.foto'] = await titoloFoto(page);
}

// Un errore del server che non passa (503 fisso, slice 044b, D-58): il Go riprova da solo dopo 5, 15,
// 30 e 60 s, e dopo 5 tentativi si ferma con «Riprova»; cambiare pagina (anche molte volte) non manda
// niente prima del momento scritto nella voce. Poi «Riprova» con il server tornato: la foto sale.
async function tentativi(page, {base, app, vista, dati}, o) {
  if (!dati) return bustaAssente(page, base, app, 1, o, 'busta-1');
  const ctx = page.context();
  page.on('dialog', d => d.accept());
  let rotto = false;
  const istanti = [];
  // Sul contesto, quindi per ogni pagina dello scenario anche dopo i cambi di pagina.
  const dellaCoda = u => /^\/repairs\/\d+\/photos$/.test(u.pathname);
  const guasto = async route => {
    if (route.request().method() !== 'POST' || !rotto) return route.fallback();
    istanti.push(ADESSO());
    return route.fulfill({status: 503, contentType: 'application/json; charset=utf-8', body: '{"error":"Foto non elaborata adesso: riprova tra poco.","ok":false}'});
  };
  await ctx.route(dellaCoda, guasto);
  fa('errore');
  await page.goto(`${base}/nuova`);
  await page.locator('#intake-photos').waitFor({state: 'visible'});
  await pronta(page, app);
  await page.locator('#intake-gallery-input').setInputFiles(foto('tentativi-044b.png', 95));
  await page.locator('[name="repair[customer_name]"]').fill('Cliente tentativi 044b');
  await page.locator('[name="repair[description]"]').fill('Orecchini dei tentativi');
  await page.locator('[name="repair[qr_code]"]').fill(`QR-044B-TENTATIVI-${vista}`);
  rotto = true;
  await page.locator('#new-repair-form button[type=submit]').click();
  await page.locator(SEL_INFO, {hasText: SALVATA}).first().waitFor();
  await pronta(page, app);
  const id = await page.locator('#photo-queue > div').first().getAttribute('data-repair-id').catch(() => null);
  // Pagine su pagine per 125 s, una ogni 2 s circa: i tentativi devono restare 5, alle loro attese.
  // La scheda è quella della busta nuova, senza foto: le miniature della busta 1 nello standalone
  // danno a volte 500 (nota 4 in docs/domande-aperte.md).
  const giro = [`${base}/`, `${base}/nuova`, `${base}/repairs/${id || 1}`];
  const fine = ADESSO() + 125000;
  for (let i = 0; ADESSO() < fine; i++) {
    await page.goto(giro[i % giro.length]).catch(() => {});
    await pronta(page, app).catch(() => {});
    await attendi(2000);
  }
  o['errore.tentativi'] = String(istanti.length);
  const pause = istanti.slice(1).map((t, i) => t - istanti[i]);
  const attese = [5000, 15000, 30000, 60000];
  o['errore.attese'] = pause.every((p, i) => p >= attese[i] - 300 && p <= attese[i] + 6000) ? 'rispettate' : pause.map(p => `${Math.round(p / 1000)} s`).join(', ') || NESSUNO;
  const dopo = istanti.length;
  for (let i = 0; i < 6; i++) {
    await page.goto(giro[i % giro.length]).catch(() => {});
    await pronta(page, app).catch(() => {});
    await attendi(1000);
  }
  await page.goto(`${base}/`);
  await pronta(page, app);
  await attendi(1000);
  o['errore.dopo-fermo'] = String(istanti.length - dopo);
  const righe = page.locator('#photo-queue > div');
  // Il numero visibile del Go (D-64, slice 053) senza etichetta è «N. 51/26», con lo spazio.
  o['errore.striscia'] = (await righe.count()) ? testo(await righe.first().innerText()).replace(/Busta (?:N\. )?\S+:/, 'Busta B1:') : NESSUNO;
  fa('riprova');
  rotto = false;
  const riprova = page.locator('#photo-queue button', {hasText: 'Riprova'});
  if (await riprova.count()) {
    await riprova.first().click();
    const finoA = ADESSO() + 60000;
    while (ADESSO() < finoA && await righe.count() > 0) await attendi(250);
  }
  o['riprova.striscia'] = (await righe.count()) ? 'non vuota' : NESSUNO;
  o['riprova.foto'] = id ? (await apriScheda(page, base, app, id), await titoloFoto(page)) : 'nessuna busta';
  await ctx.unroute(dellaCoda, guasto).catch(() => {});
  // Il 503 voluto, contato qui e tolto dai guasti.
  ctx.guasti.risposte = ctx.guasti.risposte.filter(r => !/^503 POST \/repairs\/\d+\/photos$/.test(r));
}

// La firma di consegna (RIP-S-36…40, slice 047): dalla scheda di una busta pronta «✍️ Consegna con
// firma»; sul riquadro vuoto «Conferma firma» non parte («Firma mancante.»), nemmeno dopo un tocco
// solo (la firma esiste dal primo tratto) né dopo un tratto e «Pulisci»; un tratto, «Ritirato da» e
// «Conferma firma» consegnano la busta; poi «↺ Rifai firma» con Annulla e con OK, e «Annulla» della
// pagina di firma torna alla scheda. Sul telefono si disegna col dito (eventi touch veri). Il PNG che
// il browser produce cambia con la larghezza del riquadro, che non è la stessa nelle due app: se ne
// confronta quello che conta (PNG, disegnato, il colore del tratto, l'altezza, i pixel del dispositivo
// e i byte serviti uguali a quelli mandati), mai i byte.
async function firma(page, {base, app, vista, dati}, o) {
  const busta = vista === 'desktop' ? 2 : 6;
  if (!dati) return bustaAssente(page, base, app, busta, o, `busta-${busta}`);
  const telefono = vista === 'telefono';
  const finestre = [];
  let risposta = 'ok';
  page.on('dialog', d => {
    finestre.push(`${d.type()}: ${d.message()}`);
    return risposta === 'ok' ? d.accept() : d.dismiss();
  });
  const leggiFinestre = () => {
    const v = finestre.join(' | ') || NESSUNO;
    finestre.length = 0;
    return v;
  };
  // I moduli della firma mandati (il campo firma_png di ogni POST a /repairs/N/firma).
  const invii = [];
  page.on('request', r => {
    if (r.method() === 'POST' && new URL(r.url()).pathname === `/repairs/${busta}/firma`) {
      invii.push(new URLSearchParams(r.postData() || '').get('firma_png') || '');
    }
  });
  const premi = loc => (telefono ? loc.tap() : loc.click());
  const caricata = () => page.waitForLoadState('load');
  const cdp = telefono ? await page.context().newCDPSession(page) : null;
  // Un tratto (o un tocco, senza movimento) sul riquadro: col mouse sul desktop, col dito sul telefono.
  async function tratto(muovi) {
    const b = await page.locator('#signature-pad').boundingBox();
    const punti = [[0.2, 0.5], [0.35, 0.3], [0.5, 0.6], [0.65, 0.35], [0.8, 0.55]].map(([x, y]) => [b.x + b.width * x, b.y + b.height * y]);
    const [x0, y0] = punti[0];
    if (telefono) {
      await cdp.send('Input.dispatchTouchEvent', {type: 'touchStart', touchPoints: [{x: x0, y: y0}]});
      if (muovi) {
        for (const [x, y] of punti.slice(1)) {
          await cdp.send('Input.dispatchTouchEvent', {type: 'touchMove', touchPoints: [{x, y}]});
          await attendi(30);
        }
        // Il dito si ferma prima di alzarsi, come chi firma: senza, Chrome legge il tratto come una
        // spinta (fling) e il tocco dopo serve solo a fermarla, senza clic.
        const [xf, yf] = punti[punti.length - 1];
        for (let i = 0; i < 5; i++) {
          await attendi(50);
          await cdp.send('Input.dispatchTouchEvent', {type: 'touchMove', touchPoints: [{x: xf, y: yf}]});
        }
      }
      await cdp.send('Input.dispatchTouchEvent', {type: 'touchEnd', touchPoints: []});
      await attendi(600);
    } else {
      await page.mouse.move(x0, y0);
      await page.mouse.down();
      if (muovi) for (const [x, y] of punti.slice(1)) await page.mouse.move(x, y, {steps: 4});
      await page.mouse.up();
    }
  }
  // I pixel non trasparenti del riquadro.
  const pixel = () => page.evaluate(() => {
    const c = document.getElementById('signature-pad');
    const d = c.getContext('2d').getImageData(0, 0, c.width, c.height).data;
    let n = 0;
    for (let i = 3; i < d.length; i += 4) if (d[i] > 0) n++;
    return n;
  });
  // «Conferma firma» su un riquadro che non deve partire.
  async function nonParte(chiave) {
    const prima = invii.length;
    await premi(page.locator('#signature-save'));
    await attendi(1000);
    o[`${chiave}.finestra`] = leggiFinestre();
    o[`${chiave}.percorso`] = percorso(page);
    o[`${chiave}.invii`] = String(invii.length - prima);
    o[`${chiave}.campo`] = (await page.locator('#signature-png').inputValue()) || '(vuoto)';
  }

  fa('scheda');
  await apriScheda(page, base, app, busta);
  o['scheda.pulsante'] = testo(await page.locator('#repair-signature-link').innerText());
  await Promise.all([page.waitForURL(`${base}/repairs/${busta}/firma`), premi(page.locator('#repair-signature-link'))]);
  await caricata();
  o['pagina.percorso'] = percorso(page);
  o['pagina.titolo'] = await page.title();
  o['pagina.dichiarazione'] = testo(await page.locator('#signature-declaration').innerText());
  o['pagina.lista'] = String(await page.locator('#fab-home').count());
  const riquadro = await page.locator('#signature-pad').boundingBox();
  o['pagina.riquadro-altezza'] = String(Math.round(riquadro.height));
  o['pagina.pixel'] = String(await pixel());

  fa('vuoto');
  await nonParte('vuoto');
  fa('tocco');
  await tratto(false);
  o['tocco.pixel'] = String(await pixel());
  await nonParte('tocco');
  fa('pulisci');
  await tratto(true);
  o['pulisci.prima'] = (await pixel()) > 0 ? 'disegnato' : 'vuoto';
  await page.evaluate(() => {
    window.__clicPulisci = 0;
    document.getElementById('signature-clear').addEventListener('click', () => { window.__clicPulisci++; }, true);
  });
  await premi(page.locator('#signature-clear'));
  await attendi(300);
  o['pulisci.clic'] = String(await page.evaluate(() => window.__clicPulisci));
  o['pulisci.dopo'] = String(await pixel());
  await nonParte('pulisci');

  fa('conferma');
  await tratto(true);
  await page.locator('#signature-collected-by').fill('Banco Browser 047');
  const prima = invii.length;
  await Promise.all([page.waitForURL(`${base}/repairs/${busta}`), premi(page.locator('#signature-save'))]);
  await pronta(page, app);
  o['conferma.invii'] = String(invii.length - prima);
  o['conferma.finestra'] = leggiFinestre();
  const inviata = invii[invii.length - 1] || '';
  const PREFISSO = 'data:image/png;base64,';
  o['conferma.formato'] = inviata.startsWith(PREFISSO) ? PREFISSO : inviata.slice(0, 30);
  o['conferma.percorso'] = percorso(page);
  o['conferma.messaggio'] = await messaggio(page);
  o['conferma.stato'] = testo(await page.locator('#repair-status').innerText());
  o['conferma.timbro'] = testo(await page.locator('#repair-signature-stamp').innerText())
    .replace(/\d{2}\/\d{2}\/\d{4} \d{2}:\d{2}/, 'GG/MM/AAAA hh:mm');
  await page.waitForFunction(() => {
    const img = document.getElementById('repair-signature-image');
    return img && img.complete;
  });
  o['conferma.immagine'] = await page.locator('#repair-signature-image').evaluate(img => (img.naturalWidth > 0 ? 'caricata' : 'non caricata'));
  // I byte serviti da firma.png sono quelli mandati, uguali (mai ricompressi).
  const servita = await page.evaluate(async u => {
    const r = await fetch(u, {credentials: 'same-origin'});
    const b = new Uint8Array(await r.arrayBuffer());
    let s = '';
    for (const x of b) s += String.fromCharCode(x);
    return {stato: r.status, tipo: r.headers.get('content-type'), cache: r.headers.get('cache-control'), b64: btoa(s)};
  }, `/repairs/${busta}/firma.png`);
  o['png.servito'] = `${servita.stato} ${servita.tipo} ${servita.cache}`;
  o['png.uguale-al-mandato'] = String(inviata.startsWith(PREFISSO) && servita.b64 === inviata.slice(PREFISSO.length));
  // Quello che conta del PNG del browser: si decodifica, ha il tratto (colore #102a6b), l'altezza del
  // riquadro e i pixel del dispositivo (2 per px CSS sul telefono).
  const png = await page.evaluate(async u => {
    const img = new Image();
    img.src = u;
    await img.decode();
    const c = document.createElement('canvas');
    c.width = img.naturalWidth;
    c.height = img.naturalHeight;
    const x = c.getContext('2d');
    x.drawImage(img, 0, 0);
    const d = x.getImageData(0, 0, c.width, c.height).data;
    // Il colore del tratto è il più frequente fra i pixel pieni (gli orli sfumati cambiano con la
    // geometria del riquadro).
    let pieni = 0;
    const colori = new Map();
    for (let i = 3; i < d.length; i += 4) {
      if (d[i] === 0) continue;
      pieni++;
      if (d[i] !== 255) continue;
      const k = `${d[i - 3]},${d[i - 2]},${d[i - 1]}`;
      colori.set(k, (colori.get(k) || 0) + 1);
    }
    let colore = '', n = 0;
    for (const [k, v] of colori) if (v > n) { colore = k; n = v; }
    return {w: img.naturalWidth, h: img.naturalHeight, pieni, colore};
  }, inviata.startsWith(PREFISSO) ? inviata : 'data:,');
  o['png.disegnato'] = png.pieni > 0 ? 'sì' : 'no';
  o['png.colore'] = png.colore || NESSUNO;
  o['png.altezza'] = String(png.h);
  o['png.pixel-per-px'] = String(Math.round(png.w / riquadro.width));

  fa('rifai');
  risposta = 'annulla';
  await premi(page.locator('#repair-signature-reset'));
  await attendi(1500);
  o['rifai.annulla.finestra'] = leggiFinestre();
  o['rifai.annulla.percorso'] = percorso(page);
  o['rifai.annulla.firma'] = String(await page.locator('#repair-signature-image').count());
  risposta = 'ok';
  await Promise.all([page.waitForURL(`${base}/repairs/${busta}/firma`), premi(page.locator('#repair-signature-reset'))]);
  await caricata();
  o['rifai.ok.finestra'] = leggiFinestre();
  o['rifai.ok.percorso'] = percorso(page);
  o['rifai.ok.messaggio'] = await messaggio(page);
  o['rifai.ok.pixel'] = String(await pixel());

  fa('annulla');
  await Promise.all([page.waitForURL(`${base}/repairs/${busta}`), premi(page.locator('#signature-form a', {hasText: 'Annulla'}))]);
  await pronta(page, app);
  o['annulla.percorso'] = percorso(page);
  o['annulla.pulsante'] = testo(await page.locator('#repair-signature-link').innerText());
  o['annulla.stato'] = testo(await page.locator('#repair-status').innerText());
  o['annulla.invii'] = String(invii.length);
  o['finestre-dopo'] = leggiFinestre();
}

// Il pannello delle azioni dopo la scansione (slice 054, RIP-H-23…26). Lo standalone cerca dal
// riquadro #qr-lookup della home (sul PC si apre con «📷 Scan», #qr-lookup-open; sul telefono è già lì)
// e il pannello è un evento LiveView sulla stessa pagina; il Go manda il campo unico (POST /cerca-qr,
// D-63) e la pagina della scansione ha già il pannello. Per la D-70 la conferma si apre senza
// passare dal server in tutte e due: qui si confronta quello che si vede (pulsanti, frase, avviso),
// dove porta ogni azione e il messaggio, non l'indirizzo della pagina della scansione (/cerca-qr nel
// Go, / nello standalone) né il numero della busta nel titolo (D-64).
// Le buste: sul PC la 4/26 (fuori bottega: le si dà l'etichetta RIP-2026-0058 dalla modifica della
// scheda, poi «✅ Pronta» con la conferma del rientro, RIP-H-24) e la 32/26 (pronta, «✅ Consegnata»
// con la lista filtrata per stato); sul telefono la 34/26 (compro oro, «✅ Valutata», RIP-H-26) e la
// 31/26 («💰 Preventivo», RIP-H-25, poi «▶ In lavorazione»); per la review della 054 anche la 37/26
// consegnata (solo l'avviso, sul PC) e di nuovo la 34/26, ora valutata («✅ Perfezionata» con la sua
// frase, sul telefono). Il «Sì» della 32/26 e «▶ In lavorazione» della 31/26 sono un doppio tocco
// (dblclick): l'esito è quello di un tocco, e nel Go il modulo parte una volta sola (consegna.invii e
// lavorazione.invii, solo Go: nello standalone sono eventi LiveView). Nessun altro scenario le legge
// dopo (dopo pannello-qr girano schede, sfondo, tentativi e visualizzatore, sulle 1, 2, 5, 6 e sulle
// buste nuove); lo scenario HTTP qr-pannello (solo Go, prima del browser) non le cambia.
const PULSANTI_QR = ['#qr-btn-avanza', '#qr-btn-prev', '#qr-btn-consulta', '#qr-btn-si', '#qr-btn-no'];

async function visibile(page, sel) {
  const loc = page.locator(sel);
  for (let i = 0; i < await loc.count(); i++) if (await loc.nth(i).isVisible()) return testo(await loc.nth(i).innerText());
  return NESSUNO;
}

async function cercaQR(page, base, app, codice, da) {
  await page.goto(`${base}${da}`);
  await pronta(page, app);
  if (app === 'standalone') {
    const campo = page.locator('#qr-lookup-code');
    if (!(await campo.isVisible())) await page.locator('#qr-lookup-open').click();
    await campo.waitFor({state: 'visible'});
    await campo.fill(codice);
    await campo.press('Enter');
  } else {
    const campo = page.locator('#repair-search-code');
    await campo.fill(codice);
    await campo.press('Enter');
  }
  await page.locator('#qr-action').waitFor({state: 'visible'});
  await pronta(page, app);
}

async function pannelloQR(page, o, chiave) {
  const pulsanti = [];
  for (const sel of PULSANTI_QR) {
    const t = await visibile(page, `#qr-action ${sel}`);
    if (t !== NESSUNO) pulsanti.push(t);
  }
  o[`${chiave}.stato`] = testo((await visibile(page, '#qr-action p:nth-of-type(2)')));
  o[`${chiave}.pulsanti`] = pulsanti.join(' | ') || NESSUNO;
  o[`${chiave}.conferma`] = await visibile(page, '#qr-action-confirm');
  o[`${chiave}.avviso`] = await visibile(page, '#qr-action-notice');
}

const doveSono = page => { const u = new URL(page.url()); return u.pathname + u.search; };

async function qrPannello(page, {base, app, vista, dati}, o) {
  page.__base = base;
  if (!dati) {
    await bustaAssente(page, base, app, vista === 'desktop' ? 4 : 34, o, vista === 'desktop' ? 'busta-4' : 'busta-34');
    return;
  }
  if (vista === 'desktop') {
    fa('qr-etichetta');
    await apriScheda(page, base, app, 4);
    await page.locator('#edit-repair').click();
    const qr = page.locator('#repair_qr_code');
    await qr.waitFor({state: 'visible'});
    await qr.fill('RIP-2026-0058');
    await page.locator('#save-repair').click();
    await page.locator(SEL_INFO).first().waitFor();
    await pronta(page, app);
    fa('qr-rientro');
    await cercaQR(page, base, app, 'RIP-2026-0058', '/');
    await pannelloQR(page, o, 'rientro.pannello');
    await page.locator('#qr-btn-avanza').click();
    await page.locator('#qr-action-confirm').waitFor({state: 'visible'});
    await pannelloQR(page, o, 'rientro.conferma');
    await page.locator('#qr-btn-no').click();
    await page.locator('#qr-btn-avanza').waitFor({state: 'visible'});
    await pannelloQR(page, o, 'rientro.annulla');
    await page.locator('#qr-btn-avanza').click();
    await page.locator('#qr-btn-si').waitFor({state: 'visible'});
    await Promise.all([page.waitForURL(u => new URL(u).pathname === '/repairs/4'), page.locator('#qr-btn-si').click()]);
    await pronta(page, app);
    o['rientro.dopo'] = doveSono(page);
    o['rientro.modulo'] = String(await page.locator('#repair-form').count());
    await page.goto(`${base}/`);
    await pronta(page, app);
    o['rientro.fuori-bottega'] = String(await page.locator('#dashboard-fuori-bottega').count());
    fa('qr-chiusa');
    await cercaQR(page, base, app, 'RIP-2026-0056', '/');
    await pannelloQR(page, o, 'chiusa.pannello');
    fa('qr-consegna');
    await cercaQR(page, base, app, 'RIP-2026-0053', '/?stato=pronta');
    await pannelloQR(page, o, 'consegna.pannello');
    await page.locator('#qr-btn-avanza').click();
    await page.locator('#qr-action-confirm').waitFor({state: 'visible'});
    await pannelloQR(page, o, 'consegna.conferma');
    page.context().inviiQR = 0;
    await page.locator('#qr-btn-si').dblclick();
    await page.locator(SEL_INFO, {hasText: '✓'}).first().waitFor();
    await pronta(page, app);
    o['consegna.dopo'] = doveSono(page);
    o['consegna.messaggio'] = await messaggio(page);
    o['consegna.pannello-dopo'] = await visibile(page, '#qr-action');
    await attendi(1000);
    o['consegna.invii'] = String(page.context().inviiQR || 0);
    return;
  }
  // Slice 068 (RIP-H-26): «✅ Valutata» registra l'avviso di ritiro e offre «↗ Apri WhatsApp». Il Go del
  // banco non ha indirizzo_pubblico, quindi il testo dell'avviso (con {link}) non partirebbe (D-46
  // estesa): lo scenario salva prima, nelle due app, un testo senza {link} e lo ripristina alla fine.
  page.__app = app;
  const registroPrima = {};
  for (const a of ['ready_notice.sent', 'outgoing.handed_off', 'repair.public_link_issued']) registroPrima[a] = await contaRegistro(page, base, a);
  await testoCompraOro(page, app, 'Buongiorno {nome}, la scheda compro oro n° {numero} è pronta per la firma (banco 068, senza link).');
  fa('qr-valutata');
  await cercaQR(page, base, app, 'RIP-2026-0055', '/');
  await pannelloQR(page, o, 'valutata.pannello');
  await page.locator('#qr-btn-avanza').click();
  await page.locator('#qr-action-confirm').waitFor({state: 'visible'});
  await pannelloQR(page, o, 'valutata.conferma');
  await page.locator('#qr-btn-si').click();
  await page.locator(SEL_INFO, {hasText: '✓'}).first().waitFor();
  await page.locator('#qr-send-opening').waitFor({state: 'visible'});
  await pronta(page, app);
  o['valutata.dopo'] = doveSono(page);
  o['valutata.messaggio'] = await messaggio(page);
  o['valutata.apertura.titolo'] = testo(await page.locator('#qr-opening p').first().innerText()).replace(/^#\S+/, '#N');
  o['valutata.apertura.stato'] = testo(await page.locator('#qr-opening p').nth(1).innerText());
  o['valutata.apertura.pulsanti'] = (await page.locator('#qr-opening a').allInnerTexts()).map(testo).join(' | ');
  o['valutata.apertura.target'] = String(await page.locator('#qr-send-opening').getAttribute('target'));
  o['valutata.apertura.pannello-azioni'] = String(await page.locator('#qr-action').count());
  const wa = new URL(await page.locator('#qr-send-opening').getAttribute('href'));
  o['valutata.apertura.numero'] = decodeURIComponent(wa.pathname.slice(1));
  // Il numero della busta nel testo: «34/26» nello standalone, «0055/26» nel Go (D-64, D-82): «N».
  o['valutata.apertura.wa-testo'] = JSON.stringify((wa.searchParams.get('text') || '').replace(/n° \S+/g, 'n° N'));
  fa('qr-apri-whatsapp');
  const ctx = page.context();
  const waPrima = ctx.waMe.length;
  await page.locator('#qr-send-opening').click();
  for (let i = 0; i < 100 && ctx.waMe.length === waPrima; i++) await attendi(100);
  await attendi(500);
  await pronta(page, app);
  // Le aperture si contano tutte, senza togliere i doppioni: un doppio invio si vede (review della 068).
  o['valutata.whatsapp.aperture'] = String(ctx.waMe.length - waPrima);
  o['valutata.whatsapp.dopo'] = doveSono(page);
  o['valutata.whatsapp.riquadro'] = String(await page.locator('#qr-opening').count());
  for (const p of ctx.pages()) if (p !== page) await p.close().catch(() => {});
  await apriScheda(page, base, app, 34);
  o['valutata.scheda'] = testo(await page.locator('#repair-status').innerText());
  fa('qr-perfezionata');
  await cercaQR(page, base, app, 'RIP-2026-0055', '/');
  await pannelloQR(page, o, 'perfezionata.pannello');
  await page.locator('#qr-btn-avanza').click();
  await page.locator('#qr-action-confirm').waitFor({state: 'visible'});
  await pannelloQR(page, o, 'perfezionata.conferma');
  await page.locator('#qr-btn-si').click();
  await page.locator(SEL_INFO, {hasText: '✓'}).first().waitFor();
  await pronta(page, app);
  o['perfezionata.dopo'] = doveSono(page);
  o['perfezionata.messaggio'] = await messaggio(page);
  fa('qr-preventivo');
  await cercaQR(page, base, app, 'RIP-2026-0052', '/');
  await pannelloQR(page, o, 'preventivo.pannello');
  await Promise.all([page.waitForURL(u => new URL(u).pathname === '/repairs/31'), page.locator('#qr-btn-prev').click()]);
  await pronta(page, app);
  o['preventivo.dopo'] = doveSono(page);
  o['preventivo.modulo'] = String(await page.locator('#repair-form').count());
  fa('qr-lavorazione');
  await cercaQR(page, base, app, 'RIP-2026-0052', '/');
  await pannelloQR(page, o, 'lavorazione.pannello');
  page.context().inviiQR = 0;
  await page.locator('#qr-btn-avanza').dblclick();
  await page.locator(SEL_INFO, {hasText: '✓'}).first().waitFor();
  await pronta(page, app);
  o['lavorazione.dopo'] = doveSono(page);
  o['lavorazione.messaggio'] = await messaggio(page);
  await attendi(1000);
  o['lavorazione.invii'] = String(page.context().inviiQR || 0);
  fa('qr-ripristina-testo');
  await testoCompraOro(page, app, null);
  fa('qr-registro');
  for (const a of Object.keys(registroPrima)) o[`registro.${a.replace(/[._]/g, '-')}`] = String((await contaRegistro(page, base, a)) - registroPrima[a]);
}

// testoCompraOro salva (o, con null, ripristina con la conferma) il testo «Pronta — Compro oro» dalla pagina
// «Testi dei messaggi», nelle due app (slice 068).
async function testoCompraOro(page, app, nuovo) {
  fa(nuovo === null ? 'testo-compro-oro-ripristina' : 'testo-compro-oro');
  await page.goto(`${page.__base}/settings/templates`);
  await pronta(page, app);
  if (nuovo === null) {
    page.once('dialog', d => d.accept());
    await page.locator('#template-restore-wa_pronta_compro_oro').click();
    await page.waitForFunction(() => /Testo predefinito ripristinato/.test(document.body ? document.body.innerText : ""));
  } else {
    await page.locator('#template-text-wa_pronta_compro_oro').fill(nuovo);
    await page.locator('#template-wa_pronta_compro_oro button:not([id])').click();
    await page.waitForFunction(() => /Testo salvato/.test(document.body ? document.body.innerText : ""));
  }
  await pronta(page, app);
}

// --- azioni del riquadro della lista (slice 056, RIP-H-14, 15, 16, 20) -----------------------------
//
// I pulsanti rapidi, il menu dello stato e il 📌 del riquadro, e «← Indietro» che torna alla lista
// filtrata, nelle due app. Le buste sono fuori da quelle degli altri scenari browser (pannello-qr usa
// la 4/26, 31/26, 32/26, 34/26; lettore-qr la 37/26, 5/26, 10/26 e, dalla 079, la 26/26), una serie per vista perché il
// telefono gira dopo il PC sugli stessi dati:
//   - menu: la 36/26 (PC) e la 28/26 (telefono), in lavorazione: «Preventivo» dal menu, poi di nuovo
//     «In lavorazione», così resta com'era;
//   - 📌: la 35/26 (PC) e la 33/26 (telefono), non fissate: fissata va in cima (sopra la 26/26, già
//     fissata, perché il numero è più alto), poi «Togli pin»;
//   - rapidi: la 22/26 (PC) e la 23/26 (telefono), in lavorazione: «✓ Pronta» dalla lista delle buste
//     in lavorazione porta alla scheda sul prezzo, «← Indietro» torna a quella lista; dalla lista delle
//     pronte «✓ Consegnata» chiede (Annulla, poi OK) e la busta sparisce dalla lista; «+ Nuova» e il
//     suo «← Indietro» tornano alla lista delle pronte.
// Ogni blocco parte da una pagina caricata da capo, così il messaggio di prima (che nello standalone
// resta finché la pagina non cambia) non si confonde con il suo. La frase della conferma porta il
// numero visibile, che nel Go è quello dell'etichetta (D-64, la dichiara il confronto HTTP di home):
// qui il numero diventa «N».
async function riquadroAzioni(page, {base, app, vista, dati}, o) {
  const pc = vista === 'desktop';
  const [menu, pin, rapida] = pc ? [36, 35, 22] : [28, 33, 23];
  if (!dati) return bustaAssente(page, base, app, rapida, o, `busta-${rapida}`);
  const primo = async () => (await page.locator('.rep-card').first().getAttribute('id')) || NESSUNO;
  const opzioni = async id => (await page.locator(`#transition-${id} select option`).evaluateAll(
    os => os.map(x => x.value + (x.selected ? '*' : '')))).join(' ') || NESSUNO;
  const statoCard = async id => testo(await page.locator(`#repair-status-${id}`).textContent());
  const memoria = () => page.evaluate(() => { try { return sessionStorage.getItem('gestrip:list-return') || '(vuoto)'; } catch (_e) { return '(errore)'; } });

  fa('menu');
  await page.goto(`${base}/`);
  await pronta(page, app);
  o['menu.memoria'] = await memoria();
  o['menu.prima'] = await opzioni(menu);
  await page.locator(`#transition-${menu} select`).selectOption('preventivo');
  await page.locator(`#repair-status-${menu}`, {hasText: 'Preventivo'}).waitFor();
  await pronta(page, app);
  o['menu.dopo'] = doveSono(page);
  o['menu.stato'] = await statoCard(menu);
  o['menu.opzioni'] = await opzioni(menu);
  o['menu.messaggio'] = await messaggio(page);
  o['menu.errore'] = await errore(page);
  o['menu.cambia'] = String(await page.locator(`#transition-${menu} button:visible`).count());
  await page.locator(`#transition-${menu} select`).selectOption('in_lavorazione');
  await page.locator(`#repair-status-${menu}`, {hasText: 'In lavorazione'}).waitFor();
  o['menu.ritorno.stato'] = await statoCard(menu);
  o['menu.ritorno.opzioni'] = await opzioni(menu);

  fa('pin');
  await page.goto(`${base}/`);
  await pronta(page, app);
  o['pin.prima.etichetta'] = await page.locator(`#pin-repair-${pin}`).getAttribute('aria-label');
  await page.locator(`#pin-repair-${pin}`).click();
  await page.locator(`#repair-${pin}.rep-card-pinned`).waitFor();
  await pronta(page, app);
  o['pin.primo'] = await primo();
  o['pin.etichetta'] = await page.locator(`#pin-repair-${pin}`).getAttribute('aria-label');
  o['pin.messaggio'] = await messaggio(page);
  o['pin.dopo'] = doveSono(page);
  await page.locator(`#pin-repair-${pin}`).click();
  await page.locator(`#repair-${pin}:not(.rep-card-pinned)`).waitFor();
  o['pin.tolto.primo'] = await primo();
  o['pin.tolto.etichetta'] = await page.locator(`#pin-repair-${pin}`).getAttribute('aria-label');

  fa('pronta');
  await page.goto(`${base}/?stato=in_lavorazione`);
  await pronta(page, app);
  o['pronta.memoria'] = await memoria();
  await Promise.all([page.waitForURL(u => new URL(u).pathname === `/repairs/${rapida}`), page.locator(`#quick-ready-${rapida}`).click()]);
  await pronta(page, app);
  o['pronta.dopo'] = doveSono(page);
  o['pronta.modulo'] = String(await page.locator('#repair-form').count());
  await Promise.all([page.waitForURL(u => new URL(u).pathname === '/'), page.locator('#repair-back-list').click()]);
  await pronta(page, app);
  o['pronta.indietro'] = doveSono(page);

  fa('consegna');
  let finestra = NESSUNO;
  let risposta = 'annulla';
  page.on('dialog', d => {
    finestra = `${d.type()}: ${d.message().replace(/busta \S+ come/, 'busta N come')}`;
    return risposta === 'ok' ? d.accept() : d.dismiss();
  });
  await page.goto(`${base}/?stato=pronta`);
  await pronta(page, app);
  await page.locator(`#quick-delivered-${rapida}`).click();
  await attendi(1500);
  o['consegna.annulla.finestra'] = finestra;
  o['consegna.annulla.pulsante'] = String(await page.locator(`#quick-delivered-${rapida}`).count());
  o['consegna.annulla.messaggio'] = await messaggio(page);
  finestra = NESSUNO;
  risposta = 'ok';
  await page.locator(`#quick-delivered-${rapida}`).click();
  await page.locator(`#repair-${rapida}`).waitFor({state: 'detached'});
  await pronta(page, app);
  o['consegna.ok.finestra'] = finestra;
  o['consegna.ok.dopo'] = doveSono(page);
  o['consegna.ok.messaggio'] = await messaggio(page);
  o['consegna.ok.memoria'] = await memoria();

  fa('nuova');
  await page.goto(`${base}/?stato=pronta`);
  await pronta(page, app);
  await Promise.all([page.waitForURL(u => new URL(u).pathname === '/nuova'), page.locator('#new-repair-link').click()]);
  await pronta(page, app);
  await Promise.all([page.waitForURL(u => new URL(u).pathname === '/'), page.locator('#new-repair-back').click()]);
  await pronta(page, app);
  o['nuova.indietro'] = doveSono(page);
}

// --- galleria della home (slice 057, RIP-H-13) -------------------------------------------------
//
// La vista «🖼 Galleria» nelle due app, con i dati importati: le celle, il riassunto, la linguetta
// scelta, il gettone su ogni miniatura e la prima miniatura che arriva davvero (nessuna risposta 4xx o
// 5xx, pagina.risposte-errore); la foto apre il visualizzatore della scheda su quella foto, «←
// Indietro» torna alla galleria (#list-return con vista=galleria), i dati aprono la scheda, le
// linguette passano dalla lista alla galleria e indietro. Legge e non cambia niente. Il numero
// mostrato (D-64) non si guarda, e nemmeno quante celle e quali foto: gli scenari browser di prima
// (caricamento, coda, sfondo, schede, tentativi) caricano foto in modo diverso nelle due app (D-58,
// D-59), quindi qui si guardano solo regole che valgono dentro ogni app (gettone su ogni cella,
// riassunto con il numero delle celle, il visualizzatore aperto sulla foto del link). Il contenuto
// esatto lo confronta il livello HTTP (home-galleria), sugli stessi dati.
async function galleriaHome(page, {base, app, vista, dati}, o) {
  if (!dati) return bustaAssente(page, base, app, 1, o, 'busta-1');
  const celle = page.locator('#repair-gallery > [id^="gallery-"]');
  const memoria = () => page.evaluate(() => { try { return sessionStorage.getItem('gestrip:list-return') || '(vuoto)'; } catch (_e) { return '(errore)'; } });
  const tocca = loc => (vista === 'telefono' ? loc.tap() : loc.click());
  const galleria = async () => {
    await page.goto(`${base}/?vista=galleria`);
    await pronta(page, app);
    await page.locator('#repair-gallery').waitFor();
  };

  fa('apri');
  await galleria();
  const n = await celle.count();
  o['apri.celle'] = n > 0 ? 'almeno una' : '0';
  o['apri.lista'] = String(await page.locator('#repairs').count());
  const riassunto = await leggi(page, '#gallery-summary');
  o['apri.riassunto'] = riassunto.startsWith(`${n} sched${n === 1 ? 'a' : 'e'} con foto`) ? 'le celle' : riassunto;
  o['apri.scelta'] = String(await page.locator('#view-gallery').getAttribute('aria-current'));
  o['apri.non-scelta'] = String(await page.locator('#view-list').getAttribute('aria-current'));
  o['apri.memoria'] = await memoria();
  const src = await page.locator('#repair-gallery img').evaluateAll(xs => xs.map(x => x.getAttribute('src')));
  const gettoni = src.filter(x => /[?&]g=[^&]+/.test(x) && /[?&]size=thumb(&|$)/.test(x)).length;
  o['apri.gettoni'] = gettoni === n ? 'tutte' : `${gettoni} su ${n}`;
  const riprova = await page.locator('#repair-gallery img[data-retry]').count();
  o['apri.riprova'] = riprova === n ? 'tutte' : `${riprova} su ${n}`;
  const altre = await leggi(page, '#repair-gallery .gallery-count');
  o['apri.altre'] = altre === NESSUNO || altre.split(' | ').every(x => /^\+[1-9][0-9]*$/.test(x)) ? 'forma +N' : altre;
  if (n === 0) return;
  const prima = celle.first();
  o['apri.prima-arriva'] = String(await prima.locator('img').evaluate(img => new Promise(fine => {
    const esito = () => fine(img.complete);
    if (img.complete) return esito();
    img.addEventListener('load', esito, {once: true});
    img.addEventListener('error', esito, {once: true});
    setTimeout(esito, 10000);
  })));

  fa('foto');
  const versoFoto = await prima.locator('a').first().getAttribute('href');
  await Promise.all([page.waitForURL(u => new URL(u).pathname === versoFoto.split('#')[0]), tocca(prima.locator('a').first())]);
  await pronta(page, app);
  await page.locator('.gs-viewer-frame:target').waitFor();
  const hash = await page.evaluate(() => location.hash);
  o['foto.hash'] = hash === '#' + versoFoto.split('#')[1] && /^#repair-photo-viewer-[0-9]+$/.test(hash) ? 'la foto del link' : hash;
  o['foto.aperte'] = String(await page.locator('.gs-viewer-frame:target').count());

  fa('indietro');
  await page.goto(page.url().split('#')[0]);
  await pronta(page, app);
  await Promise.all([page.waitForURL(u => new URL(u).pathname === '/'), page.locator('#repair-back-list').click()]);
  await pronta(page, app);
  o['indietro.dopo'] = doveSono(page);
  o['indietro.galleria'] = String(await page.locator('#repair-gallery').count());

  fa('dati');
  await galleria();
  const versoDati = await celle.first().locator('a').nth(1).getAttribute('href');
  await Promise.all([page.waitForURL(u => new URL(u).pathname === versoDati), tocca(celle.first().locator('a').nth(1))]);
  await pronta(page, app);
  o['dati.dopo'] = doveSono(page) === versoDati && /^\/repairs\/[0-9]+$/.test(versoDati) ? 'la scheda della cella' : doveSono(page);
  o['dati.hash'] = (await page.evaluate(() => location.hash)) || NESSUNO;

  fa('linguette');
  await galleria();
  await Promise.all([page.waitForURL(u => new URL(u).search === ''), page.locator('#view-list').click()]);
  await pronta(page, app);
  o['linguette.lista.dopo'] = doveSono(page);
  o['linguette.lista.righe'] = String(await page.locator('#repairs').count());
  o['linguette.lista.galleria'] = String(await page.locator('#repair-gallery').count());
  await Promise.all([page.waitForURL(u => new URL(u).search === '?vista=galleria'), page.locator('#view-gallery').click()]);
  await pronta(page, app);
  o['linguette.galleria.dopo'] = doveSono(page);
  o['linguette.galleria.celle'] = (await celle.count()) === n ? 'le stesse' : 'altre';
}

// --- lettore QR con la fotocamera finta (slice 055, RIP-H-27, RIP-H-28, D-52) -------------------
//
// Il lettore con la fotocamera nelle due app, con la fotocamera finta di Chromium
// (--use-fake-device-for-media-stream e --use-file-for-fake-video-capture): un Chromium per
// fotocamera, perché il file si sceglie all'avvio. I file Y4M li scrive `banco esegui` (fotocamera.go)
// in /banco/fotocamere: «diritto» e «trasposto» sono il QR di RIP-2026-0056 (la 37/26 consegnata dei
// dati importati; nel database vuoto non è di nessuna busta) diritto e a specchio sulla diagonale,
// «etichetta» è il QR dell'etichetta 5 di Chromium dello standalone (internal/pdf/lettura/testdata,
// trasposto, SINT-QR-0005). Le prove:
//   - diritto, PC, home: 📷, la lettura, il pannello sopra la lista (o il messaggio), l'indirizzo che
//     non cambia, il suono dell'esito del server, «Chiudi» e la lista ancora lì;
//   - diritto, telefono, «Nuova riparazione»: il codice nel campo «Etichetta», il bip di successo;
//   - trasposto, PC, home, ed etichetta, telefono, «Nuova riparazione»: D-52, il QR a specchio sulla
//     diagonale. Lo leggono tutte e due le app: jsQR 1.4.0 lo riprova trasposto da sé;
//   - etichetta, PC, home: SINT-QR-0005 è delle gemelle 5/26 e 10/26 dei dati importati, aperte: la
//     scelta nello stesso riquadro sopra la lista, poi il pannello della prima (la più recente).
// Dalla slice 059, con le fotocamere finte di Chromium senza file (un disegno che non ha nessun QR):
//   - vuota, PC, home, pistola (RIP-H-27): il tocco su 📷 sblocca l'audio e «Chiudi»; poi il codice
//     scritto nel campo come lo scrive il lettore a pistola, con Invio. Nel Go il campo unico (D-63),
//     nello standalone il campo «Cerca per QR busta». Un'etichetta di nessuna busta (in minuscolo,
//     «rip-2026-0999») suona i due bip gravi nelle due app; un nome no nel Go (solo_go: lo standalone
//     lo legge come un QR sconosciuto);
//   - nessuna, telefono, «Nuova riparazione», senza (RIP-H-28): un Chromium senza fotocamera; il 📷
//     di «Etichetta» dice il messaggio del browser nella finestra, e chiusa la finestra («Chiudi» o
//     Esc) il cursore è nel campo per la pistola (solo_go: lo standalone lo lascia sulla pagina);
//   - due, telefono, «Nuova riparazione», cambia (RIP-H-28): due fotocamere finte; «🔄 Cambia
//     fotocamera» passa all'altra, la ricorda (gs-camera-qr) e di nuovo torna alla prima.
// Dalla slice 079 (ART-09), con consegna.y4m (fotocamera.go: la 26/26, RIP-2026-0047, il bianco,
// SINT-NIENTE-079 di nessuna busta, il bianco, di nuovo la 26/26 e il bianco):
//   - consegna, PC e telefono, lo scanner continuo della sessione di consegna (letturaConsegna).
// Dalla slice 090 (ORE-07), con lotto.y4m (la stessa sequenza con il QR di CODICE_LOTTO, la busta propria della prova):
//   - lotto, PC e telefono, lo scanner continuo della pagina del lotto d'avviso (letturaLotto).
// Il suono si osserva dagli oscillatori Web Audio creati (la frequenza di ogni tono) e dalle
// vibrazioni chieste, con uno script messo prima di quelli della pagina.
const FOTOCAMERE = '/banco/fotocamere';
const PROVE_LETTORE = [
  {fotocamera: 'diritto', vista: 'desktop', prova: 'home'},
  {fotocamera: 'trasposto', vista: 'desktop', prova: 'home'},
  {fotocamera: 'etichetta', vista: 'desktop', prova: 'gemelle'},
  {fotocamera: 'diritto', vista: 'telefono', prova: 'nuova'},
  {fotocamera: 'etichetta', vista: 'telefono', prova: 'nuova'},
  {fotocamera: 'vuota', vista: 'desktop', prova: 'pistola'},
  {fotocamera: 'nessuna', vista: 'telefono', prova: 'senza'},
  {fotocamera: 'due', vista: 'telefono', prova: 'cambia'},
  {fotocamera: 'consegna', vista: 'desktop', prova: 'scanner'},
  {fotocamera: 'consegna', vista: 'telefono', prova: 'scanner'},
  {fotocamera: 'lotto', vista: 'desktop', prova: 'scansione'},
  {fotocamera: 'lotto', vista: 'telefono', prova: 'scansione'},
];
// argomentiFotocamera sono le opzioni di Chromium per la fotocamera della prova: un file Y4M, il disegno
// finto di Chromium (una o due fotocamere) o nessuna fotocamera.
function argomentiFotocamera(nome) {
  // Senza la finta richiesta del permesso il Chromium senza finestra risponde «Not supported».
  const finta = ['--use-fake-device-for-media-stream', '--use-fake-ui-for-media-stream'];
  switch (nome) {
    case 'nessuna': return [];
    case 'vuota': return finta;
    case 'due': return ['--use-fake-device-for-media-stream=device-count=2', '--use-fake-ui-for-media-stream'];
    default: return [...finta, `--use-file-for-fake-video-capture=${FOTOCAMERE}/${nome}.y4m`];
  }
}
// Il tempo per leggere: la fotocamera finta mostra il QR dal primo fotogramma; chi non lo legge
// entro questo tempo non lo legge più.
const TEMPO_LETTURA = 12000;
const SUONI = `(() => {
  const segnala = (cosa, valore) => window.__segnala && window.__segnala(cosa, valore || '', Date.now());
  const Base = window.BaseAudioContext || window.AudioContext;
  if (Base && Base.prototype.createOscillator) {
    const crea = Base.prototype.createOscillator;
    Base.prototype.createOscillator = function () {
      const o = crea.call(this);
      const imposta = o.frequency.setValueAtTime.bind(o.frequency);
      o.frequency.setValueAtTime = (hz, t) => { segnala('tono', String(hz)); return imposta(hz, t); };
      return o;
    };
  }
  if (navigator.vibrate) {
    const vibra = navigator.vibrate.bind(navigator);
    navigator.vibrate = p => { segnala('vibra', JSON.stringify(p)); return vibra(p); };
  }
})()`;

const eventiDi = (ctx, cosa) => ctx.eventi.filter(e => e.cosa === cosa).map(e => e.valore).join(' ') || NESSUNO;

async function letturaHome(page, {base, app}, o, k) {
  const ctx = page.context();
  await page.goto(`${base}/`);
  await pronta(page, app);
  const scan = page.locator('#qr-lookup-scan');
  if (app === 'standalone' && !(await scan.isVisible())) await page.locator('#qr-lookup-open').click();
  await scan.waitFor({state: 'visible'});
  ctx.eventi = [];
  await scan.click();
  const esito = page.locator('#qr-action, #qr-matches-count, #qr-lookup-error');
  const letto = await esito.first().waitFor({state: 'visible', timeout: TEMPO_LETTURA}).then(() => true, () => false);
  if (letto) await pronta(page, app);
  await attendi(800);
  o[`${k}.letto`] = String(letto);
  await pannelloQR(page, o, `${k}.pannello`);
  o[`${k}.errore`] = await visibile(page, '#qr-lookup-error');
  o[`${k}.finestra`] = await visibile(page, '#qr-scanner-status');
  o[`${k}.percorso`] = doveSono(page);
  o[`${k}.lista`] = String(await page.locator('#repairs').count());
  o[`${k}.suono`] = eventiDi(ctx, 'tono');
  o[`${k}.vibra`] = eventiDi(ctx, 'vibra');
  if (!letto) {
    await page.locator('#qr-scanner-close').click();
    return;
  }
  await page.locator('#qr-lookup-close').click();
  await page.locator('#qr-action, #qr-lookup-error').first().waitFor({state: 'hidden'});
  await pronta(page, app);
  o[`${k}.chiuso.pannello`] = await visibile(page, '#qr-action, #qr-lookup-error');
  o[`${k}.chiuso.percorso`] = doveSono(page);
  o[`${k}.chiuso.lista`] = String(await page.locator('#repairs').count());
}

async function letturaGemelle(page, {base, app}, o, k) {
  const ctx = page.context();
  await page.goto(`${base}/`);
  await pronta(page, app);
  const scan = page.locator('#qr-lookup-scan');
  if (app === 'standalone' && !(await scan.isVisible())) await page.locator('#qr-lookup-open').click();
  await scan.waitFor({state: 'visible'});
  ctx.eventi = [];
  await scan.click();
  await page.locator('#qr-matches-count, #qr-action, #qr-lookup-error').first().waitFor({state: 'visible', timeout: TEMPO_LETTURA});
  await pronta(page, app);
  await attendi(800);
  o[`${k}.conteggio`] = await visibile(page, '#qr-matches-count');
  o[`${k}.scelte`] = String(await page.locator('#qr-match-list button').count());
  o[`${k}.errore`] = await visibile(page, '#qr-lookup-error');
  o[`${k}.lista`] = String(await page.locator('#repairs').count());
  o[`${k}.suono`] = eventiDi(ctx, 'tono');
  if (await page.locator('#qr-match-list button').count() === 0) return;
  ctx.eventi = [];
  await page.locator('#qr-match-list button').first().click();
  await page.locator('#qr-action').waitFor({state: 'visible'});
  await pronta(page, app);
  await attendi(800);
  await pannelloQR(page, o, `${k}.scelta`);
  o[`${k}.scelta.lista`] = String(await page.locator('#repairs').count());
  o[`${k}.scelta.percorso`] = doveSono(page);
  o[`${k}.scelta.elenco`] = await visibile(page, '#qr-match-list');
  o[`${k}.scelta.suono`] = eventiDi(ctx, 'tono');
}

async function letturaNuova(page, {base, app}, o, k) {
  const ctx = page.context();
  await page.goto(`${base}/nuova`);
  await pronta(page, app);
  const campo = page.locator('#repair_qr_code');
  await campo.waitFor({state: 'visible'});
  ctx.eventi = [];
  await page.locator('#repair-qr-code-scan').click();
  const fine = ADESSO() + TEMPO_LETTURA;
  while (!(await campo.inputValue()) && ADESSO() < fine) await attendi(200);
  await attendi(800);
  o[`${k}.campo`] = (await campo.inputValue()) || NESSUNO;
  o[`${k}.finestra`] = await visibile(page, '#qr-scanner-status');
  o[`${k}.percorso`] = doveSono(page);
  o[`${k}.suono`] = eventiDi(ctx, 'tono');
  o[`${k}.vibra`] = eventiDi(ctx, 'vibra');
  if (await page.locator('#qr-scanner-close').isVisible()) await page.locator('#qr-scanner-close').click();
}

// sbloccaSuono tocca 📷 della home (come in negozio il primo tocco: sblocca l'audio) e chiude la
// finestra, con la fotocamera finta che non ha nessun QR.
async function sbloccaSuono(page, base, app) {
  await page.goto(`${base}/`);
  await pronta(page, app);
  const scan = page.locator('#qr-lookup-scan');
  if (app === 'standalone' && !(await scan.isVisible())) await page.locator('#qr-lookup-open').click();
  await scan.waitFor({state: 'visible'});
  await scan.click();
  await page.locator('#qr-scanner-status', {hasText: 'Inquadra'}).waitFor();
  await page.locator('#qr-scanner-close').click();
  await page.locator('#qr-scanner-status').waitFor({state: 'hidden'});
}

async function letturaPistola(page, {base, app}, o, k) {
  const ctx = page.context();
  for (const [nome, testo] of [['sconosciuta', 'rip-2026-0999'], ['testo', 'Cliente inesistente']]) {
    await sbloccaSuono(page, base, app);
    const campo = page.locator(app === 'go' ? '#repair-search-code' : '#qr-lookup-code');
    await campo.click();
    await campo.fill(testo);
    ctx.eventi = [];
    await campo.press('Enter');
    // Nel Go la lista della ricerca (con la sua pillola), nello standalone il messaggio del QR.
    await page.locator(app === 'go' ? '#active-repair-filters' : '#qr-lookup-error').first().waitFor({state: 'visible'});
    await pronta(page, app);
    await attendi(800);
    o[`${k}.${nome}.suono`] = eventiDi(ctx, 'tono');
    o[`${k}.${nome}.vibra`] = eventiDi(ctx, 'vibra');
    o[`${k}.${nome}.percorso`] = doveSono(page);
  }
}

const idDelFuoco = page => page.evaluate(() => {
  const el = document.activeElement;
  return el && el !== document.body ? (el.id || el.tagName.toLowerCase()) : 'body';
});

async function letturaSenza(page, {base, app}, o, k) {
  await page.goto(`${base}/nuova`);
  await pronta(page, app);
  const campo = page.locator('#repair_qr_code');
  await campo.waitFor({state: 'visible'});
  for (const [nome, chiudi] of [['chiudi', () => page.locator('#qr-scanner-close').click()], ['esc', () => page.keyboard.press('Escape')]]) {
    await page.locator('#repair-qr-code-scan').click();
    await page.locator('#qr-scanner-status', {hasText: '⚠'}).waitFor();
    o[`${k}.${nome}.messaggio`] = await visibile(page, '#qr-scanner-status');
    await chiudi();
    await page.locator('#qr-scanner-status').waitFor({state: 'hidden', timeout: 3000}).catch(() => {});
    o[`${k}.${nome}.finestra`] = await visibile(page, '#qr-scanner-status');
    o[`${k}.${nome}.fuoco`] = await idDelFuoco(page);
    if (await page.locator('#qr-scanner-close').isVisible()) await page.locator('#qr-scanner-close').click();
    await page.evaluate(() => document.activeElement && document.activeElement.blur && document.activeElement.blur());
  }
  o[`${k}.campo`] = (await campo.inputValue()) || NESSUNO;
  o[`${k}.percorso`] = doveSono(page);
}

const etichettaCamera = page => page.evaluate(() => {
  const v = document.querySelector('#qr-scanner-modal video');
  const t = v && v.srcObject && v.srcObject.getVideoTracks()[0];
  return t && t.readyState === 'live' ? t.label : '(nessuna)';
});

async function letturaCambia(page, {base, app}, o, k) {
  await page.goto(`${base}/nuova`);
  await pronta(page, app);
  await page.locator('#repair-qr-code-scan').click();
  await page.locator('#qr-scanner-status', {hasText: 'Inquadra'}).waitFor();
  const prima = await etichettaCamera(page);
  o[`${k}.prima`] = prima;
  const cambia = async quella => {
    await page.locator('#qr-scanner-switch').click();
    await page.waitForFunction(q => {
      const v = document.querySelector('#qr-scanner-modal video');
      const t = v && v.srcObject && v.srcObject.getVideoTracks()[0];
      return t && t.readyState === 'live' && t.label !== q;
    }, quella, {timeout: 5000}).catch(() => {});
    await attendi(300);
    return etichettaCamera(page);
  };
  const dopo = await cambia(prima);
  o[`${k}.dopo`] = dopo;
  o[`${k}.stato`] = await visibile(page, '#qr-scanner-status');
  o[`${k}.ricordata`] = await page.evaluate(() => {
    try { return localStorage.getItem('gs-camera-qr') ? 'sì' : 'no'; } catch (_e) { return 'storage rotto'; }
  });
  o[`${k}.ancora`] = await cambia(dopo);
  await page.locator('#qr-scanner-close').click();
  await page.locator('#qr-scanner-status').waitFor({state: 'hidden'});
  o[`${k}.chiusa`] = await page.evaluate(() => {
    const v = document.querySelector('#qr-scanner-modal video');
    return v && v.srcObject && v.srcObject.getTracks().some(t => t.readyState === 'live') ? 'accesa' : 'spenta';
  });
  o[`${k}.percorso`] = doveSono(page);
}

// fasiDelloScanner (slice 079, 090) tocca il 📷 dello scanner continuo e annota le fasi che la fotocamera finta provoca
// (aggiunta, codice sconosciuto, busta di nuovo), ognuna con il messaggio della riga di stato, i toni e le vibrazioni; la
// finestra resta aperta. Le fasi sono [nome, regex della riga di stato] e vanno in ordine: ognuna è la prima riga di stato
// che le somiglia dopo quella della fase prima (una riga della fase dopo arrivata prima non conta). I toni e le vibrazioni
// di una fase sono quelli entro un secondo dalla sua riga. conN toglie il numero della busta dai messaggi.
async function fasiDelloScanner(page, o, k, pulsante, FASI, conN) {
  const ctx = page.context();
  await page.evaluate(STATI_LETTORE);
  ctx.eventi = [];
  await page.locator(pulsante).click();
  const fasi = eventi => {
    const trovate = [];
    let dopo = -Infinity;
    for (const [, re] of FASI) {
      const e = eventi.find(x => x.cosa === 'stato' && x.t > dopo && re.test(x.valore));
      trovate.push(e || null);
      if (!e) break;
      dopo = e.t;
    }
    return trovate;
  };
  const fine = ADESSO() + TEMPO_SCANNER;
  while (fasi(ctx.eventi).filter(Boolean).length < FASI.length && ADESSO() < fine) await attendi(250);
  await attendi(500);
  const eventi = [...ctx.eventi];
  const trovate = fasi(eventi);
  FASI.forEach(([nome], i) => {
    const e = trovate[i];
    o[`${k}.${nome}.messaggio`] = e ? conN(e.valore) : NESSUNO;
    const vicini = cosa => (e && eventi.filter(x => x.cosa === cosa && Math.abs(x.t - e.t) <= 1000).map(x => x.valore).join(' ')) || NESSUNO;
    o[`${k}.${nome}.suono`] = vicini('tono');
    o[`${k}.${nome}.vibra`] = vicini('vibra');
  });
  o[`${k}.finestra`] = (await page.locator('#qr-scanner-status').isVisible()) ? 'aperta' : 'chiusa';
}

// letturaConsegna (slice 079, ART-09): «📷 Scansiona» della sessione di consegna, nelle due app. Sul database
// vuoto non c'è un artigiano: si guarda solo il menu. Con i dati, la sessione dell'artigiano 1 (le consegne di
// sessioni-consegna sono confermate: sul PC se ne apre una nuova, sul telefono si riprende quella del PC, vuota
// dopo ✕); nel campo «Numero busta» un testo che non si manda; il 📷 tocca e sblocca l'audio; la finestra resta
// aperta e la fotocamera finta mostra a turno la 26/26 e un codice di nessuna busta. Le righe di stato della
// finestra si annotano con l'istante in cui compaiono, come i toni (SUONI): per la busta aggiunta, il codice
// sconosciuto e la busta di nuovo («già in lista», quando torna nel video) il messaggio con il conto e i toni
// entro un secondo; le fasi in ordine, ognuna dopo quella prima. Poi «Chiudi»: la pagina con la lista e il conto
// di adesso, solo l'ultimo errore (il flash verde dello standalone si chiude al tocco su «Chiudi»,
// FlashAutoClose), il testo del campo com'era. Sul PC ✕
// toglie la 26/26 (la sessione resta vuota per il telefono); sul telefono la conferma, e la sessione
// confermata non ha più «📷 Scansiona». Il numero della busta nei messaggi si scrive «#N» (D-64: «#26/26»
// nello standalone, «#0047/26» nel Go). Gira per ultima: la 26/26 resta fuori bottega solo alla fine.
const STATI_LETTORE = `(() => {
  const segnala = (cosa, valore) => window.__segnala && window.__segnala(cosa, valore || '', Date.now());
  let prima = null;
  const guarda = () => {
    const el = document.getElementById('qr-scanner-status');
    const t = el ? (el.textContent || '').replace(/\\s+/g, ' ').trim() : null;
    if (t && t !== prima) segnala('stato', t);
    prima = t;
  };
  new MutationObserver(guarda).observe(document, {subtree: true, childList: true, characterData: true});
})()`;
const TEMPO_SCANNER = 45000;
async function letturaConsegna(page, {base, app, dati}, o, k, vista) {
  const conN = s => s.replace(/#[0-9]+\/[0-9]{2}/g, '#N');
  await page.goto(`${base}/handovers`);
  await pronta(page, app);
  if (!dati) {
    o[`${k}.artigiani`] = String((await page.locator('#handover-artisan-id option').count()) - 1);
    return;
  }
  page.on('dialog', d => d.accept());
  await page.locator('#handover-artisan-id').selectOption('1');
  await page.locator('#new-handover-form button[type="submit"]').click();
  await page.waitForURL(/\/handovers\/[0-9]+$/);
  await pronta(page, app);
  const sessione = percorso(page);
  const conta = async () => testo(await page.locator('#handover-bag-count').innerText());
  o[`${k}.prima.conteggio`] = await conta();
  o[`${k}.pulsante`] = testo(await page.locator('#handover-scan-button').innerText());
  await page.locator('#handover-bag-number').fill('0099/26');
  await fasiDelloScanner(page, o, k, '#handover-scan-button', [['aggiunta', /^✓ Aggiunta/], ['sconosciuto', /non trovata/], ['ripetuta', /già in lista/]], conN);
  await page.locator('#qr-scanner-close').click();
  await page.locator('#qr-scanner-status').waitFor({state: 'hidden'}).catch(() => {});
  await page.waitForFunction(() => (document.getElementById('handover-bag-count') || {}).textContent === '1', null, {timeout: 10000}).catch(() => {});
  await page.locator(SEL_ERRORE).first().waitFor({timeout: 10000}).catch(() => {});
  await pronta(page, app);
  await attendi(500);
  o[`${k}.chiusa.percorso`] = percorso(page) === sessione ? 'la sessione' : percorso(page);
  o[`${k}.chiusa.conteggio`] = await conta();
  o[`${k}.chiusa.righe`] = (await page.locator('#handover-bags li').allInnerTexts()).map(t => conN(testo(t))).join(' | ') || NESSUNO;
  o[`${k}.chiusa.messaggio`] = conN(await messaggio(page));
  o[`${k}.chiusa.errore`] = await errore(page);
  o[`${k}.chiusa.campo`] = JSON.stringify(await page.locator('#handover-bag-number').inputValue());
  if (vista === 'desktop') {
    await page.locator('#handover-remove-26').click();
    await page.waitForFunction(() => (document.getElementById('handover-bag-count') || {}).textContent === '0');
    await pronta(page, app);
    o[`${k}.togli.conteggio`] = await conta();
    return;
  }
  await page.locator('#handover-confirm').click();
  await page.waitForURL(u => new URL(u).search === '?fuori_bottega=1');
  await pronta(page, app);
  await page.goto(`${base}${sessione}`);
  await pronta(page, app);
  o[`${k}.confermata.pulsante`] = String(await page.locator('#handover-scan-button').count());
}

// letturaLotto (slice 090, ORE-07): «📷 Scansiona» della pagina del lotto d'avviso, nelle due app, con lotto.y4m (fotocamera.go:
// il QR di SINT-LOTTO-090, il bianco, SINT-NIENTE-079 di nessuna busta, il bianco, di nuovo la prima e il bianco). Sul
// database vuoto non c'è un lotto: si guarda solo il titolo della lista. Con i dati la prova ha **la sua** oreficeria «Zz Banco
// 090» e la sua busta (QR SINT-LOTTO-090, la sola del banco con quel codice, del 2025, pronta): le crea il PC (le oreficerie non
// si cancellano) e il telefono le trova, ma se manca l'oreficeria o la busta le crea anche lui (il PC si è fermato prima). Si apre il suo lotto da
// /ready-lots (sul telefono è lo stesso del PC, vuoto dopo «Rimuovi»); nel campo «Numero busta» un testo che non si manda; il 📷
// tocca e sblocca l'audio; la finestra resta aperta e la fotocamera mostra a turno la busta, un codice di nessuna busta e di
// nuovo la busta («già in questo lotto»). Le righe di stato si annotano come per la consegna (fasiDelloScanner): messaggio con
// il conto «nel lotto» e toni entro un secondo; nel lotto, come nello standalone, «già in questo lotto» è un errore e suona i
// due bip gravi. Poi «Chiudi»: la pagina con la lista e il conto di adesso, **l'ultimo errore** («↻ Busta già in questo lotto»,
// il flash rosso resta, il verde dello standalone si chiude al tocco su «Chiudi»), il testo del campo com'era (D-98: la lista
// sotto la finestra non si confronta durante la scansione). Le righe del
// registro ready_lot.bag_added si contano prima e dopo (una: la ripetizione non scrive). Sul PC «Rimuovi» rimette la busta fra le
// candidate e il lotto vuoto per il telefono; sul telefono la busta resta nel lotto (oreficeria e lotto sono solo di questa prova).
const CODICE_LOTTO = 'SINT-LOTTO-090';
async function letturaLotto(page, {base, app, dati}, o, k, vista) {
  const conN = s => s.replace(/(#|N\. )[0-9]+\/[0-9]{2}/g, '#N');
  const nome = 'Zz Banco 090';
  await page.goto(`${base}/ready-lots`);
  await pronta(page, app);
  if (!dati) {
    o[`${k}.lista`] = testo(await page.locator('h1').first().innerText());
    return;
  }
  if (!(await page.locator('#ready-lot-goldsmith option', {hasText: nome}).count())) await oreficeriaPropria(page, base, app, nome, '');
  const prima = await contaRegistro(page, base, 'ready_lot.bag_added');
  const apriLotto = async () => {
    await page.goto(`${base}/ready-lots`);
    await pronta(page, app);
    await page.locator('#ready-lot-goldsmith').selectOption({label: nome});
    await page.locator('#ready-lot-open-form button[type="submit"]').click();
    await page.waitForURL(/\/ready-lots\/[0-9]+$/);
    await pronta(page, app);
    return percorso(page);
  };
  let lotto = await apriLotto();
  // La busta della prova (fra le candidate o già nel lotto): se manca (il PC si è fermato prima di crearla) la crea questa vista.
  if (!(await page.locator('#ready-lot-candidates li, #ready-lot-bags li', {hasText: 'Anello lotto 090'}).count())) {
    await bustaPropria(page, base, app, {nome, cliente: 'Cliente lotto 090', descrizione: 'Anello lotto 090', qr: CODICE_LOTTO, numero25: '8100/25', portala: true});
    lotto = await apriLotto();
  }
  const conta = async () => testo(await page.locator('#ready-lot-bag-count').innerText());
  o[`${k}.prima.conteggio`] = await conta();
  o[`${k}.pulsante`] = testo(await page.locator('#ready-lot-scan-button').innerText());
  await page.locator('#ready-lot-bag-number').fill('0099/26');
  await fasiDelloScanner(page, o, k, '#ready-lot-scan-button', [['aggiunta', /^✓ Aggiunta/], ['sconosciuto', /non trovata/], ['ripetuta', /già in questo lotto/]], conN);
  await page.locator('#qr-scanner-close').click();
  await page.locator('#qr-scanner-status').waitFor({state: 'hidden'}).catch(() => {});
  await page.waitForFunction(() => (document.getElementById('ready-lot-bag-count') || {}).textContent === '1', null, {timeout: 10000}).catch(() => {});
  await page.locator(SEL_ERRORE).first().waitFor({timeout: 10000}).catch(() => {});
  await pronta(page, app);
  await attendi(500);
  o[`${k}.chiusa.percorso`] = percorso(page) === lotto ? 'il lotto' : percorso(page);
  o[`${k}.chiusa.conteggio`] = await conta();
  o[`${k}.chiusa.righe`] = (await page.locator('#ready-lot-bags li').allInnerTexts()).map(t => conN(testo(t))).join(' | ') || NESSUNO;
  o[`${k}.chiusa.messaggio`] = conN(await messaggio(page));
  o[`${k}.chiusa.errore`] = await errore(page);
  o[`${k}.chiusa.campo`] = JSON.stringify(await page.locator('#ready-lot-bag-number').inputValue());
  o[`${k}.registro.ready-lot-bag-added`] = String((await contaRegistro(page, base, 'ready_lot.bag_added')) - prima);
  if (vista === 'desktop') {
    await page.goto(`${base}${lotto}`);
    await pronta(page, app);
    await page.locator('#ready-lot-bags li button').first().click();
    await page.waitForFunction(() => (document.getElementById('ready-lot-bag-count') || {}).textContent === '0');
    await pronta(page, app);
    o[`${k}.togli.conteggio`] = await conta();
  }
}

// lettore fa le prove con un Chromium per fotocamera; le osservazioni di una vista sono di tutte le
// sue prove, con il nome della fotocamera e della prova davanti ai campi.
async function lettore(app, nomeApp, dati, cred, uscita) {
  for (const p of PROVE_LETTORE) {
    const o = ((uscita.osservazioni[nomeApp]['lettore-qr'] ??= {})[p.vista] ??= {});
    const k = `${p.fotocamera}.${p.prova}`;
    const browser = await chromium.launch({headless: true, chromiumSandbox: true, args: argomentiFotocamera(p.fotocamera)});
    try {
      const base = app[nomeApp];
      const ctx = await contesto(browser, base, p.vista);
      await ctx.grantPermissions(['camera'], {origin: base});
      await ctx.addInitScript(SUONI);
      const page = await ctx.newPage();
      fa(`lettore-${k}`);
      try {
        await accedi(ctx, base, nomeApp, cred);
        const prova = {home: letturaHome, gemelle: letturaGemelle, nuova: letturaNuova, pistola: letturaPistola,
          senza: letturaSenza, cambia: letturaCambia, scanner: letturaConsegna, scansione: letturaLotto}[p.prova];
        await prova(page, {base, app: nomeApp, dati}, o, k, p.vista);
      } catch (e) {
        const riga = String(e && e.message || e).split('\n')[0].replace(/https:\/\/[^\s/]+/g, '<app>');
        uscita.errori.push(`lettore-qr ${p.vista} ${nomeApp} (fase ${fase}): ${e && e.name}: ${riga}`);
      } finally {
        await page.close().catch(() => {});
        const g = {};
        scriviGuasti(ctx, g);
        for (const [c, v] of Object.entries(g)) o[`${p.fotocamera}.${c}`] = v;
        await ctx.close().catch(() => {});
      }
    } finally {
      await browser.close().catch(() => {});
    }
  }
}

// --- il popup «📲 Avvisa cliente» (slice 066; CLI-29…36, RIP-H-17, RIP-S-20, RIP-S-23, D-74) -------
//
// Nelle due app, con i dati importati. Desktop, dalla lista «In lavorazione» sulla 25/26: il 📲 apre il
// popup sopra la lista (l'indirizzo non cambia), «Annulla» ed Esc lo chiudono, «Invia» con il testo
// corretto apre WhatsApp (l'indirizzo wa.me si annota e non si raggiunge) e la pagina torna alla lista
// con i filtri (D-74); la scheda dice «inviato il»; il popup della 2/26 (oreficeria) si legge e si
// annulla. Telefono, sulla 39/26 (9999/25): la risposta apre il popup dell'aggiornamento, poi la busta
// va pronta, l'avviso di ritiro si manda (lista con i filtri dopo), la scheda ha «📲 Avvisato il» e la
// storia, «Azzera l'avviso» (con la conferma) la rimette da avvisare; alla fine torna in lavorazione.
// Il numero della busta nei testi (D-64: «25/26» nello standalone, «0045/25» nel Go) si scrive «N», il
// link della busta «<link>», le date e le ore «<data>»; le righe del registro si contano prima e dopo.
const LINK_BUSTA = /https:\/\/[^\s/]+\/r\/[A-Za-z0-9_-]+/g;
const DATA_ORA = /[0-9]{2}\/[0-9]{2}\/[0-9]{4}( [0-9]{2}:[0-9]{2})?/g;

async function contaRegistro(page, base, azione) {
  await page.goto(`${base}/audit?action=${azione}`);
  return Number((testo(await page.locator('#audit-total').innerText()).match(/^(\d+)/) || [0, -1])[1]);
}

async function leggiPopup(page, o, k) {
  await page.locator('#notice-popup').waitFor();
  const numero = testo(await page.locator('#notice-title + p').innerText()).replace(/^Busta /, '');
  // D-82 (slice 067): nei testi del Go il numero senza «N.» davanti («Busta N. 1/26», testo «n° 1/26»).
  // Le due forme diventano «N», quindi il banco browser non controlla la D-82: la controllano i test Go
  // (TestNumeroNeiTestiD82, TestApriAvvisoClienteDiretto, TestPopupDallaScheda).
  const nudo = numero.replace(/^N\. /, '');
  const via = s => s.split(`n° ${numero}`).join('n° N').split(`n. ${numero}`).join('n. N')
    .split(`n° ${nudo}`).join('n° N').split(`n. ${nudo}`).join('n. N');
  o[`${k}.titolo`] = testo(await page.locator('#notice-title').innerText());
  o[`${k}.piattaforma`] = testo(await page.locator('#notice-platform').innerText());
  o[`${k}.problema`] = await leggi(page, '#notice-problem');
  const corpo = page.locator('#notice-body');
  o[`${k}.testo`] = (await corpo.count()) ? JSON.stringify(via(await corpo.inputValue())) : NESSUNO;
  o[`${k}.suggerimento`] = (await corpo.count()) ? testo(await page.locator('#notice-body + p').innerText()) : NESSUNO;
  o[`${k}.pulsanti`] = (await page.locator('#notice-send, #notice-cancel').allInnerTexts()).map(testo).join(' | ') || NESSUNO;
  return via;
}

// inviaPopup preme «Invia» e aspetta la lista (D-74) e l'indirizzo wa.me della scheda aperta al tocco.
async function inviaPopup(page, o, k, via) {
  const ctx = page.context();
  const prima = ctx.waMe.length;
  await Promise.all([page.waitForURL(u => new URL(u).pathname === '/', {timeout: 20000}), page.locator('#notice-send').click()]);
  await pronta(page, page.__app);
  for (let i = 0; i < 100 && ctx.waMe.length === prima; i++) await attendi(100);
  o[`${k}.dopo`] = doveSono(page);
  // Le aperture si contano tutte, senza togliere i doppioni: un doppio invio si vede (review della 068).
  o[`${k}.aperture`] = String(ctx.waMe.length - prima);
  const u = ctx.waMe.length > prima ? new URL(ctx.waMe[ctx.waMe.length - 1]) : null;
  o[`${k}.numero`] = u ? decodeURIComponent(u.pathname.slice(1)) : NESSUNO;
  o[`${k}.wa-testo`] = u ? JSON.stringify(via(u.searchParams.get('text') || '').replace(LINK_BUSTA, '<link>')) : NESSUNO;
  for (const p of ctx.pages()) if (p !== page) await p.close().catch(() => {});
}

async function whatsapp(page, {base, app, vista, dati}, o) {
  const pc = vista === 'desktop';
  const busta = pc ? 25 : 39;
  if (!dati) return bustaAssente(page, base, app, busta, o, `busta-${busta}`);
  page.__app = app;
  // Slice 070b: anche le righe dell'email di pronta (CLI-42), che senza un indirizzo confermato nessuna
  // delle due app scrive al passaggio a «pronta».
  const azioni = ['outgoing.handed_off', 'ready_notice.sent', 'ready_notice.reset', 'repair.public_link_issued',
    'repair.email_ready_queued', 'repair.email_ready_not_queued'];
  const prima = {};
  for (const a of azioni) prima[a] = await contaRegistro(page, base, a);
  const sparito = () => page.locator('#notice-popup').waitFor({state: 'detached'});

  if (pc) {
    fa('lista');
    await page.goto(`${base}/?stato=in_lavorazione`);
    await pronta(page, app);
    await page.locator(`#notify-repair-${busta}`).click();
    let via = await leggiPopup(page, o, 'lista');
    o['lista.percorso'] = doveSono(page);
    await page.locator('#notice-cancel').click();
    await sparito();
    o['annulla.percorso'] = doveSono(page);
    await page.locator(`#notify-repair-${busta}`).click();
    await page.locator('#notice-popup').waitFor();
    await page.keyboard.press('Escape');
    await sparito();
    o['esc.percorso'] = doveSono(page);
    fa('invio');
    await page.locator(`#notify-repair-${busta}`).click();
    via = await leggiPopup(page, o, 'invio.popup');
    // Senza {link}: il Go del banco non ha indirizzo_pubblico, e un testo con il link lì non parte
    // (ErrLinkNonSalvato, come la D-46); il link lo provano i test del Go.
    await page.locator('#notice-body').fill('Messaggio del banco 066, senza link');
    await inviaPopup(page, o, 'invio', via);
    await apriScheda(page, base, app, busta);
    o['invio.link'] = testo(await page.locator('#repair-public-link-state').innerText()).replace(DATA_ORA, '<data>');
    fa('oreficeria');
    await apriScheda(page, base, app, 2);
    o['oreficeria.pulsante'] = testo(await page.locator('#repair-notify').innerText());
    await page.locator('#repair-notify').click();
    await leggiPopup(page, o, 'oreficeria');
    await page.locator('#notice-cancel').click();
    await sparito();
    o['oreficeria.annulla.percorso'] = percorso(page);
  } else {
    fa('risposta');
    await page.goto(`${base}/?stato=in_lavorazione`);
    await pronta(page, app);
    await apriScheda(page, base, app, busta);
    await page.locator('#repair-reply-body').fill('Risposta del banco 066');
    await page.locator('#repair-reply button').click();
    await leggiPopup(page, o, 'risposta');
    o['risposta.messaggio'] = await messaggio(page);
    await page.locator('#notice-cancel').click();
    await sparito();
    await pronta(page, app);
    o['risposta.annulla.percorso'] = percorso(page);
    fa('pronta');
    await apriScheda(page, base, app, busta);
    await page.locator('#transition_status').selectOption('pronta');
    await page.locator('#repair-status-form button').click();
    await page.locator('#repair-notice').waitFor();
    await pronta(page, app);
    o['pronta.riquadro'] = testo(await page.locator('#repair-notice').innerText());
    o['pronta.pulsante'] = testo(await page.locator('#repair-notify').innerText());
    // Slice 070b: nessun indirizzo, nessun riquadro «📧 Avviso email» nella scheda (EmailNotice.sheet/1).
    o['pronta.email'] = String(await page.locator('#repair-email-notice').count());
    fa('ritiro');
    await page.locator('#repair-notify').click();
    const via = await leggiPopup(page, o, 'ritiro.popup');
    await page.locator('#notice-body').fill((await page.locator('#notice-body').inputValue()).replace('{link}', 'in negozio'));
    await inviaPopup(page, o, 'ritiro', via);
    await apriScheda(page, base, app, busta);
    o['ritiro.badge'] = testo(await page.locator('#repair-notice-badge').innerText()).replace(DATA_ORA, '<data>');
    o['ritiro.storia'] = (await page.locator('#repair-notice-history li').allInnerTexts()).map(x => testo(x).replace(DATA_ORA, '<data>')).join(' | ') || NESSUNO;
    o['ritiro.link'] = testo(await page.locator('#repair-public-link-state').innerText()).replace(DATA_ORA, '<data>');
    fa('azzera');
    let finestra = NESSUNO;
    page.on('dialog', d => { finestra = `${d.type()}: ${d.message()}`; return d.accept(); });
    await page.locator('#repair-notice-reset').click();
    await page.locator('#repair-notice-pending').waitFor();
    await pronta(page, app);
    o['azzera.finestra'] = finestra;
    o['azzera.messaggio'] = await messaggio(page);
    o['azzera.storia'] = (await page.locator('#repair-notice-history li').allInnerTexts()).map(x => testo(x).replace(DATA_ORA, '<data>')).join(' | ') || NESSUNO;
    fa('ritorno');
    await apriScheda(page, base, app, busta);
    await page.locator('#transition_status').selectOption('in_lavorazione');
    await page.locator('#repair-status-form button').click();
    await page.locator('#repair-status', {hasText: 'In lavorazione'}).waitFor();
  }
  fa('registro');
  for (const a of azioni) o[`registro.${a.replace(/[._]/g, '-')}`] = String((await contaRegistro(page, base, a)) - prima[a]);
}

// testi-invii (slice 067): la pagina «Testi dei messaggi» e la lista «📨 Invii registrati», nelle due app.
// Desktop: un testo con un segnaposto non ammesso (messaggio, riquadro dell'errore, anteprima), uno
// valido (messaggio, casella), «Ripristina predefinito» con la conferma (messaggio, casella di nuovo il
// predefinito: wa_pronta resta com'era per gli scenari dopo). Su tutte e due le viste, sulla 24/26 (0046/26,
// aperta, di nessun altro scenario): un messaggio senza link dal popup, poi la scheda con la lista (titolo,
// didascalia, riga, «Togli dalla lista»), «Togli dalla lista» con la conferma (messaggio, lista sparita), e
// le righe del registro outgoing.template_saved e outgoing.handoff_discarded contate prima e dopo.
async function testiInvii(page, {base, app, vista, dati}, o) {
  const busta = 24;
  if (!dati) return bustaAssente(page, base, app, busta, o, `busta-${busta}`);
  page.__app = app;
  const azioni = ['outgoing.template_saved', 'outgoing.handoff_discarded', 'outgoing.handed_off'];
  const prima = {};
  for (const a of azioni) prima[a] = await contaRegistro(page, base, a);
  let finestra = NESSUNO;
  page.on('dialog', d => { finestra = `${d.type()}: ${d.message()}`; return d.accept(); });
  const casella = '#template-text-wa_pronta';
  if (vista === 'desktop') {
    fa('testi');
    await page.goto(`${base}/settings/templates`);
    await pronta(page, app);
    o['testi.titolo'] = testo(await page.locator('h1').first().innerText());
    o['testi.sezioni'] = (await page.locator('section[id^="template-section-"] h3').allInnerTexts()).map(testo).join(' | ');
    fa('testi-rifiuto');
    await page.locator(casella).fill('Ciao {nome}, {password}');
    await page.locator('#template-wa_pronta button:not([id])').click();
    await page.locator('#template-error-wa_pronta').waitFor();
    await pronta(page, app);
    o['rifiuto.errore'] = await errore(page);
    o['rifiuto.riquadro'] = testo(await page.locator('#template-error-wa_pronta').innerText());
    o['rifiuto.anteprima'] = testo(await page.locator('#template-preview-wa_pronta').innerText());
    o['rifiuto.casella'] = JSON.stringify(await page.locator(casella).inputValue());
    fa('testi-salva');
    await page.locator(casella).fill('Ciao {nome},\nla busta {numero} è pronta. {link}');
    await page.locator('#template-wa_pronta button:not([id])').click();
    await page.locator('#template-error-wa_pronta').waitFor({state: 'detached'});
    await page.waitForFunction(() => /Testo salvato/.test(document.body ? document.body.innerText : ""));
    await pronta(page, app);
    o['salva.messaggio'] = await messaggio(page);
    o['salva.casella'] = JSON.stringify(await page.locator(casella).inputValue());
    o['salva.anteprima'] = JSON.stringify(await page.locator('#template-preview-wa_pronta').innerText());
    fa('testi-ripristina');
    await page.locator('#template-restore-wa_pronta').click();
    await page.waitForFunction(() => /Testo predefinito ripristinato/.test(document.body ? document.body.innerText : ""));
    await pronta(page, app);
    o['ripristina.finestra'] = finestra;
    o['ripristina.messaggio'] = await messaggio(page);
    o['ripristina.casella'] = JSON.stringify(await page.locator(casella).inputValue());
  }
  fa('invio');
  await apriScheda(page, base, app, busta);
  o['prima.lista'] = String(await page.locator('#repair-send-history').count());
  await page.locator('#repair-notify').click();
  const via = await leggiPopup(page, o, 'popup');
  await page.locator('#notice-body').fill(`Messaggio del banco 067 (${vista}), senza link`);
  await inviaPopup(page, o, 'invio', via);
  fa('lista');
  await apriScheda(page, base, app, busta);
  const lista = page.locator('#repair-send-history');
  o['lista.titolo'] = testo(await lista.locator('summary').innerText());
  await lista.locator('summary').click();
  o['lista.didascalie'] = (await lista.locator('p[id^="repair-send-caption-"]').allInnerTexts()).map(testo).join(' | ') || NESSUNO;
  const riga = lista.locator('li[id^="repair-send-message-"]').first();
  o['lista.riga'] = testo(await riga.innerText()).replace(DATA_ORA, '<data>');
  const togli = riga.locator('[id^="repair-send-discard-"]');
  o['lista.togli'] = testo(await togli.innerText());
  fa('togli');
  finestra = NESSUNO;
  await togli.click();
  await page.locator('#repair-send-history').waitFor({state: 'detached'});
  await pronta(page, app);
  o['togli.finestra'] = finestra;
  o['togli.messaggio'] = await messaggio(page);
  o['togli.lista'] = String(await page.locator('#repair-send-history').count());
  fa('registro');
  for (const a of azioni) o[`registro.${a.replace(/[._]/g, '-')}`] = String((await contaRegistro(page, base, a)) - prima[a]);
}

// anagrafica-artigiani (slice 076): la parte «Artigiani» di /settings/anagrafiche, nelle due app (le
// oreficerie clienti della stessa pagina le guarda anagrafica-oreficerie, slice 086). Sul database
// vuoto solo la pagina. Con i dati, per vista: un artigiano nuovo con spazi e un telefono da normalizzare (con un
// doppio tocco su «+ Aggiungi»: un invio solo), lo
// stesso nome di nuovo (doppione), un nome di soli spazi (errore sotto il campo), la modifica in un nome già
// usato con altre maiuscole e poi in uno nuovo, «Elimina» rifiutato sul primo artigiano importato per nome (ha
// delle buste) e «Elimina» sull'artigiano nuovo, con le conferme; le righe del registro contate prima e dopo. Alla
// fine l'anagrafica è com'era.
async function anagraficaArtigiani(page, {base, app, vista, dati}, o) {
  page.__app = app;
  const pagina = async () => {
    await page.goto(`${base}/settings/anagrafiche`);
    await pronta(page, app);
  };
  const righe = () => page.locator('#artisans-list > [id^="artisan-"]');
  const attendi = async t => {
    await page.waitForFunction(x => !!document.body && document.body.innerText.includes(x), t);
    await pronta(page, app);
  };
  // La riga dell'artigiano con quel nome nel campo (il valore di adesso, non l'attributo).
  const rigaDi = async nome => {
    const rr = righe();
    for (let i = 0; i < await rr.count(); i++) {
      if (await rr.nth(i).locator('input[name="artisan[name]"]').inputValue() === nome) return rr.nth(i);
    }
    return null;
  };
  fa('pagina');
  await pagina();
  o['pagina.titolo'] = testo(await page.locator('h1').first().innerText());
  o['pagina.riquadro'] = testo(await page.locator('#artigiani h2').innerText());
  o['pagina.vuoto'] = String(await page.locator('#artisans-empty').isVisible());
  o['pagina.vuoto-testo'] = testo(await page.locator('#artisans-empty').textContent());
  o['pagina.nuovo-etichette'] = (await page.locator('#new-artisan-form label').allInnerTexts()).map(testo).join(' | ');
  o['pagina.nuovo-segnaposto'] = `${await page.locator('#new-artisan-name').getAttribute('placeholder')} | ${await page.locator('#new-artisan-phone').getAttribute('placeholder')}`;
  o['pagina.nuovo-pulsante'] = testo(await page.locator('#new-artisan-form button[type="submit"]').innerText());
  const iniziali = await righe().count();
  o['pagina.righe'] = String(iniziali);
  if (!dati) return;
  const azioni = ['artisan.created', 'artisan.updated', 'artisan.deleted'];
  const prima = {};
  for (const a of azioni) prima[a] = await contaRegistro(page, base, a);
  let finestra = NESSUNO;
  page.on('dialog', d => { finestra = `${d.type()}: ${d.message()}`; return d.accept(); });
  const nome = `Artigiano Banco ${vista}`;
  fa('crea');
  await pagina();
  o['pagina.prima-riga'] = await righe().first().locator('input[name="artisan[name]"]').inputValue();
  await page.locator('#new-artisan-name').fill(`  ${nome}  `);
  await page.locator('#new-artisan-phone').fill(' +39 000 (076) 00-01 ');
  // Doppio tocco su «+ Aggiungi» (review della 076): un invio solo, nessun «Esiste già…».
  await page.locator('#new-artisan-form button[type="submit"]').dblclick();
  await attendi(`Artigiano "${nome}" aggiunto.`);
  await page.waitForTimeout(1000);
  o['crea.messaggio'] = await messaggio(page);
  o['crea.errore'] = await errore(page);
  o['crea.righe'] = String(await righe().count());
  const riga = await rigaDi(nome);
  o['crea.riga'] = String(riga !== null);
  o['crea.telefono'] = riga ? await riga.locator('input[name="artisan[phone]"]').inputValue() : NESSUNO;
  o['crea.campo-nome'] = JSON.stringify(await page.locator('#new-artisan-name').inputValue());
  fa('doppione');
  await page.locator('#new-artisan-name').fill(`   ${nome}   `);
  await page.locator('#new-artisan-phone').fill(' +39 333-1 ');
  await page.locator('#new-artisan-form button[type="submit"]').click();
  await attendi(`Esiste già un artigiano con il nome "${nome}".`);
  o['doppione.errore'] = await errore(page);
  o['doppione.campo-nome'] = JSON.stringify(await page.locator('#new-artisan-name').inputValue());
  o['doppione.campo-telefono'] = JSON.stringify(await page.locator('#new-artisan-phone').inputValue());
  o['doppione.righe'] = String(await righe().count());
  fa('vuoto');
  await page.locator('#new-artisan-name').fill('   ');
  await page.locator('#new-artisan-phone').fill(' 333 (0) ');
  await page.locator('#new-artisan-form button[type="submit"]').click();
  await attendi('Il nome è obbligatorio.');
  o['vuoto.errore'] = await errore(page);
  o['vuoto.sotto-il-campo'] = (await page.locator('#new-artisan-form p').allInnerTexts()).map(testo).filter(Boolean).join(' | ') || NESSUNO;
  o['vuoto.campo-nome'] = JSON.stringify(await page.locator('#new-artisan-name').inputValue());
  o['vuoto.campo-telefono'] = JSON.stringify(await page.locator('#new-artisan-phone').inputValue());
  fa('modifica-doppione');
  await pagina();
  let mia = await rigaDi(nome);
  const altro = o['pagina.prima-riga'];
  await mia.locator('input[name="artisan[name]"]').fill(altro.toUpperCase());
  await mia.locator('input[name="artisan[phone]"]').fill(' +39 333-1 ');
  await mia.locator('button[type="submit"]:not([id])').click();
  await attendi(`Esiste già un artigiano con il nome "${altro.toUpperCase()}".`);
  o['modifica-doppione.errore'] = await errore(page).then(s => s.replace(altro.toUpperCase(), 'ALTRO'));
  const rifiutata = await rigaDi(altro.toUpperCase());
  o['modifica-doppione.campo'] = String(rifiutata !== null);
  o['modifica-doppione.campo-telefono'] = rifiutata ? JSON.stringify(await rifiutata.locator('input[name="artisan[phone]"]').inputValue()) : NESSUNO;
  fa('modifica');
  await pagina();
  mia = await rigaDi(nome);
  await mia.locator('input[name="artisan[name]"]').fill(`${nome} bis`);
  await mia.locator('input[name="artisan[phone]"]').fill('(02) 12.34');
  await mia.locator('button[type="submit"]:not([id])').click();
  await attendi('Artigiano aggiornato.');
  o['modifica.messaggio'] = await messaggio(page);
  mia = await rigaDi(`${nome} bis`);
  o['modifica.telefono'] = mia ? await mia.locator('input[name="artisan[phone]"]').inputValue() : NESSUNO;
  fa('elimina-rifiutato');
  await pagina();
  finestra = NESSUNO;
  await (await rigaDi(altro)).locator('[id^="delete-artisan-"]').click();
  await attendi('Impossibile eliminare');
  o['elimina-rifiutato.finestra'] = finestra.replace(altro, 'ALTRO');
  o['elimina-rifiutato.errore'] = await errore(page);
  o['elimina-rifiutato.righe'] = String(await righe().count());
  fa('elimina');
  await pagina();
  finestra = NESSUNO;
  await (await rigaDi(`${nome} bis`)).locator('[id^="delete-artisan-"]').click();
  await attendi('Artigiano eliminato.');
  o['elimina.finestra'] = finestra;
  o['elimina.messaggio'] = await messaggio(page);
  o['elimina.righe'] = String(await righe().count());
  o['elimina.come-prima'] = String(await righe().count() === iniziali);
  fa('registro');
  for (const a of azioni) o[`registro.${a.replace(/[._]/g, '-')}`] = String((await contaRegistro(page, base, a)) - prima[a]);
}

// anagrafica-oreficerie (slice 086): la parte «Oreficerie clienti» di /settings/anagrafiche, nelle due app (ORE-01…03).
// Le oreficerie non si eliminano: quelle che lo scenario crea restano per tutto il giro (anche nei menu delle altre
// pagine), quindi i nomi sono propri per vista e cadono in fondo all'ordinamento («Zz Banco 086 desktop» e «… telefono»),
// e nessuno scenario dopo legge i menu delle oreficerie (nessuno scenario browser lo fa; gli HTTP girano prima). Sul
// database vuoto solo la pagina. Con i dati, per vista: una oreficeria nuova con spazi e un telefono da normalizzare (con
// un doppio tocco su «+ Aggiungi»: un invio solo), lo stesso nome di nuovo (doppione), il nome del negozio (la riga 1,
// «Negozio», non si vede ma l'indice unico la conosce), un nome di soli spazi (errore sotto il campo), lo stesso nome con
// altre maiuscole (alla creazione è un nome nuovo), la modifica di quella in un nome già usato con altre maiuscole
// (alla modifica è un doppione) e poi in uno nuovo. Il campo del nome dopo un «+ Aggiungi» riuscito (`crea.campo-nome`) è una
// differenza D-88 (estensione della D-88 a ORE-02, in attesa della conferma di Stefano: Dubbio 2 della 086). Le righe del registro si contano prima e dopo.
// Alla fine ci sono due oreficerie in più per vista (`fine.righe-in-piu`). Righe iniziali con i dati (`pagina.righe`): 3 sul
// PC (quelle della cattura) e 7 sul telefono, nelle due app: le 2 del PC di questo scenario, quella di lotti-apertura
// (slice 088, «Zz Banco 088 desktop») e quella di lotti-buste (slice 089, «Zz Banco 089 desktop»), che sul PC girano dopo
// questo e prima di tutte le viste del telefono (ordine dei nomi, prima tutto il PC). Anche lotti-apertura e lotti-buste
// leggono il menu delle oreficerie, ma solo la prima voce e la loro per nome.
async function anagraficaOreficerie(page, {base, app, vista, dati}, o) {
  page.__app = app;
  const pagina = async () => {
    await page.goto(`${base}/settings/anagrafiche`);
    await pronta(page, app);
  };
  const righe = () => page.locator('#oreficerie-list > div[id^="oreficerie-"]');
  const attendi = async t => {
    await page.waitForFunction(x => !!document.body && document.body.innerText.includes(x), t);
    await pronta(page, app);
  };
  // La riga dell'oreficeria con quel nome nel campo (il valore di adesso, non l'attributo).
  const rigaDi = async nome => {
    const rr = righe();
    for (let i = 0; i < await rr.count(); i++) {
      if (await rr.nth(i).locator('input[name="oreficeria[name]"]').inputValue() === nome) return rr.nth(i);
    }
    return null;
  };
  const invia = () => page.locator('#new-oreficeria-form button[type="submit"]').click();
  fa('pagina');
  await pagina();
  o['pagina.titolo'] = testo(await page.locator('h1').first().innerText());
  o['pagina.riquadro'] = testo(await page.locator('#oreficerie h2').innerText());
  o['pagina.vuoto'] = String(await page.locator('#oreficerie-empty').isVisible());
  o['pagina.vuoto-testo'] = testo(await page.locator('#oreficerie-empty').textContent());
  o['pagina.nuovo-etichette'] = (await page.locator('#new-oreficeria-form label').allInnerTexts()).map(testo).join(' | ');
  o['pagina.nuovo-segnaposto'] = `${await page.locator('#new-oreficeria-name').getAttribute('placeholder')} | ${await page.locator('#new-oreficeria-phone').getAttribute('placeholder')}`;
  o['pagina.nuovo-pulsante'] = testo(await page.locator('#new-oreficeria-form button[type="submit"]').innerText());
  o['pagina.elimina'] = String(await page.locator('#oreficerie button', {hasText: 'Elimina'}).count());
  o['pagina.negozio'] = String((await rigaDi('Negozio')) !== null);
  const iniziali = await righe().count();
  o['pagina.righe'] = String(iniziali);
  if (!dati) return;
  const azioni = ['oreficeria.created', 'oreficeria.updated', 'oreficeria.deleted'];
  const prima = {};
  for (const a of azioni) prima[a] = await contaRegistro(page, base, a);
  const nome = `Zz Banco 086 ${vista}`;
  const maiuscolo = nome.toUpperCase();
  fa('crea');
  await pagina();
  o['pagina.prima-riga'] = await righe().first().locator('input[name="oreficeria[name]"]').inputValue();
  await page.locator('#new-oreficeria-name').fill(`  ${nome}  `);
  await page.locator('#new-oreficeria-phone').fill(' +39 000 (086) 00-01 ');
  // Doppio tocco su «+ Aggiungi»: un invio solo, nessun «Esiste già…».
  await page.locator('#new-oreficeria-form button[type="submit"]').dblclick();
  await attendi(`Oreficeria "${nome}" aggiunta.`);
  await page.waitForTimeout(1000);
  o['crea.messaggio'] = await messaggio(page);
  o['crea.errore'] = await errore(page);
  o['crea.righe-in-piu'] = String((await righe().count()) - iniziali);
  const riga = await rigaDi(nome);
  o['crea.riga'] = String(riga !== null);
  o['crea.campo-nome'] = JSON.stringify(await page.locator('#new-oreficeria-name').inputValue());
  o['crea.telefono'] = riga ? await riga.locator('input[name="oreficeria[phone]"]').inputValue() : NESSUNO;
  o['crea.salva'] = riga ? (await riga.locator('button').allInnerTexts()).map(testo).join(' | ') : NESSUNO;
  fa('doppione');
  await page.locator('#new-oreficeria-name').fill(`   ${nome}   `);
  await page.locator('#new-oreficeria-phone').fill(' +39 333-1 ');
  await invia();
  await attendi(`Esiste già un'oreficeria con il nome "${nome}".`);
  o['doppione.errore'] = await errore(page);
  o['doppione.campo-nome'] = JSON.stringify(await page.locator('#new-oreficeria-name').inputValue());
  o['doppione.campo-telefono'] = JSON.stringify(await page.locator('#new-oreficeria-phone').inputValue());
  o['doppione.righe-in-piu'] = String((await righe().count()) - iniziali);
  fa('negozio');
  await page.locator('#new-oreficeria-name').fill('Negozio');
  await page.locator('#new-oreficeria-phone').fill('');
  await invia();
  await attendi(`Esiste già un'oreficeria con il nome "Negozio".`);
  o['negozio.errore'] = await errore(page);
  fa('vuoto');
  await page.locator('#new-oreficeria-name').fill('   ');
  await page.locator('#new-oreficeria-phone').fill(' 333 (0) ');
  await invia();
  await attendi('Il nome è obbligatorio.');
  o['vuoto.errore'] = await errore(page);
  o['vuoto.sotto-il-campo'] = (await page.locator('#new-oreficeria-form p').allInnerTexts()).map(testo).filter(Boolean).join(' | ') || NESSUNO;
  o['vuoto.campo-nome'] = JSON.stringify(await page.locator('#new-oreficeria-name').inputValue());
  o['vuoto.campo-telefono'] = JSON.stringify(await page.locator('#new-oreficeria-phone').inputValue());
  fa('maiuscole');
  await pagina();
  await page.locator('#new-oreficeria-name').fill(maiuscolo);
  await page.locator('#new-oreficeria-phone').fill('');
  await invia();
  await attendi(`Oreficeria "${maiuscolo}" aggiunta.`);
  o['maiuscole.messaggio'] = await messaggio(page);
  o['maiuscole.errore'] = await errore(page);
  o['maiuscole.righe-in-piu'] = String((await righe().count()) - iniziali);
  fa('modifica-doppione');
  await pagina();
  let mia = await rigaDi(maiuscolo);
  const minuscolo = nome.toLowerCase();
  await mia.locator('input[name="oreficeria[name]"]').fill(minuscolo);
  await mia.locator('input[name="oreficeria[phone]"]').fill(' +39 333-1 ');
  await mia.locator('button[type="submit"]').click();
  await attendi(`Esiste già un'oreficeria con il nome "${minuscolo}".`);
  o['modifica-doppione.errore'] = await errore(page);
  const rifiutata = await rigaDi(minuscolo);
  o['modifica-doppione.campo'] = String(rifiutata !== null);
  o['modifica-doppione.campo-telefono'] = rifiutata ? JSON.stringify(await rifiutata.locator('input[name="oreficeria[phone]"]').inputValue()) : NESSUNO;
  fa('modifica');
  await pagina();
  mia = await rigaDi(maiuscolo);
  await mia.locator('input[name="oreficeria[name]"]').fill(`${nome} bis`);
  await mia.locator('input[name="oreficeria[phone]"]').fill('(02) 12.34');
  await mia.locator('button[type="submit"]').click();
  await attendi('Oreficeria aggiornata.');
  o['modifica.messaggio'] = await messaggio(page);
  mia = await rigaDi(`${nome} bis`);
  o['modifica.telefono'] = mia ? await mia.locator('input[name="oreficeria[phone]"]').inputValue() : NESSUNO;
  o['modifica.vecchio-nome'] = String((await rigaDi(maiuscolo)) !== null);
  fa('fine');
  await pagina();
  o['fine.elimina'] = String(await page.locator('#oreficerie button', {hasText: 'Elimina'}).count());
  o['fine.righe-in-piu'] = String((await righe().count()) - iniziali);
  o['fine.negozio'] = String((await rigaDi('Negozio')) !== null);
  fa('registro');
  for (const a of azioni) o[`registro.${a.replace(/[._]/g, '-')}`] = String((await contaRegistro(page, base, a)) - prima[a]);
}

// lotti-apertura (slice 088): la lista dei lotti d'avviso /ready-lots e l'apertura di un lotto, nelle due app (ORE-04,
// ORE-05). Sul database vuoto solo la pagina («Nessuna oreficeria in anagrafica.», «Nessun lotto aperto oggi.»). Con i dati,
// per vista, su un'oreficeria creata dallo scenario («Zz Banco 088 desktop» / «… telefono»: le oreficerie non si
// cancellano, il nome cade in fondo all'ordinamento come quelli di anagrafica-oreficerie): «Apri lotto» senza scelta, poi
// con tre valori forgiati nel menu (un'opzione aggiunta dallo scenario, come un invio manomesso: «abc», la riga 1 del
// negozio e un id che non c'è), poi l'apertura (l'indirizzo /ready-lots/L), la riga del lotto nella lista («0 buste»,
// l'ora dell'apertura si scrive «<data>») e la ripresa dello stesso lotto. Dalla 089 l'apertura arriva alla pagina vera del
// lotto (il titolo, `apri.titolo`): la pagina finta e il giro del POST del Go della 088 non servono più. Il lotto resta aperto,
// vuoto: lo cancella la pulizia dei giorni dopo; lotti-buste (089) gira dopo questo, nella stessa vista, su un'oreficeria sua
// (gli HTTP lotti e lotti-dipendente girano prima); sul telefono il lotto del PC è già in lista (`pagina.vuoto` «(nessuno)»), quindi prima di aprire si contano solo
// le righe della propria oreficeria (`apri.righe-prima`, 0). Le righe ready_lot.opened si contano prima e dopo: una per vista.
async function lottiApertura(page, {base, app, vista, dati}, o) {
  page.__app = app;
  const lista = async () => {
    await page.goto(`${base}/ready-lots`);
    await pronta(page, app);
  };
  const attendi = async t => {
    await page.waitForFunction(x => !!document.body && document.body.innerText.includes(x), t);
    await pronta(page, app);
  };
  const apri = () => page.locator('#ready-lot-open-form button[type="submit"]').click();
  // forgia mette nel menu un'opzione con quel valore e la sceglie (LiveView o il ridisegno del Go la tolgono dopo).
  const forgia = async valore => {
    await page.evaluate(v => {
      const s = document.getElementById('ready-lot-goldsmith');
      if (![...s.options].some(x => x.value === v)) s.add(new Option(`forgiata ${v}`, v));
    }, valore);
    await page.locator('#ready-lot-goldsmith').selectOption(valore);
  };
  fa('pagina');
  await lista();
  o['pagina.titolo'] = testo(await page.locator('h1').first().innerText());
  o['pagina.frase'] = testo(await page.locator('h1 + p').first().innerText());
  o['pagina.menu-prima'] = testo(await page.locator('#ready-lot-goldsmith option').first().innerText());
  // Il testo dell'etichetta senza le opzioni del menu (nelle due app la <label> avvolge la <select>).
  o['pagina.etichetta'] = testo(await page.locator('label[for="ready-lot-goldsmith"]').evaluate(l => [...l.childNodes].filter(n => n.nodeName !== 'SELECT').map(n => n.textContent).join(' ')));
  o['pagina.pulsante'] = testo(await page.locator('#ready-lot-open-form button[type="submit"]').innerText());
  o['pagina.lotti'] = testo(await page.locator('#ready-lot-list h2').innerText());
  o['pagina.vuoto'] = (await page.locator('#ready-lot-list-empty').count()) ? testo(await page.locator('#ready-lot-list-empty').innerText()) : NESSUNO;
  o['pagina.nessuna'] = (await page.locator('#ready-lot-no-goldsmith').count()) ? testo(await page.locator('#ready-lot-no-goldsmith').innerText()) : NESSUNO;
  if (!dati) return;
  const nome = `Zz Banco 088 ${vista}`;
  fa('oreficeria');
  await page.goto(`${base}/settings/anagrafiche`);
  await pronta(page, app);
  await page.locator('#new-oreficeria-name').fill(nome);
  await page.locator('#new-oreficeria-form button[type="submit"]').click();
  await attendi(`Oreficeria "${nome}" aggiunta.`);
  const prima = await contaRegistro(page, base, 'ready_lot.opened');
  fa('senza-scelta');
  await lista();
  await apri();
  await attendi("Seleziona un'oreficeria.");
  o['senza-scelta.errore'] = await errore(page);
  for (const [k, v, t] of [['non-valida', 'abc', 'Oreficeria non valida.'], ['negozio', '1', "Non si può creare un avviso per l'oreficeria principale."],
    ['non-trovata', '987654', 'Oreficeria non trovata.']]) {
    fa(k);
    await lista();
    await forgia(v);
    await apri();
    await attendi(t);
    o[`${k}.errore`] = await errore(page);
    o[`${k}.titolo`] = testo(await page.locator('h1').first().innerText());
  }
  fa('apri');
  await lista();
  // Prima dell'apertura nessuna riga con la propria oreficeria (sul telefono il lotto del PC resta in lista).
  o['apri.righe-prima'] = String(await page.locator('#ready-lot-list li', {hasText: nome}).count());
  await page.locator('#ready-lot-goldsmith').selectOption({label: nome});
  await apri();
  await page.waitForURL(/\/ready-lots\/[0-9]+$/);
  await pronta(page, app);
  const lotto = percorso(page);
  o['apri.percorso'] = lotto.replace(/[0-9]+$/, 'L');
  // Dalla 089 la pagina del lotto c'è nelle due app.
  o['apri.titolo'] = testo(await page.locator('h1').first().innerText());
  fa('lista');
  await lista();
  const riga = page.locator('#ready-lot-list li', {hasText: nome});
  o['lista.righe'] = String(await riga.count());
  o['lista.riga'] = (await riga.count()) ? testo(await riga.first().innerText()).replace(/#[0-9]+/, '#L').replace(DATA_ORA, '<data>') : NESSUNO;
  o['lista.apri'] = (await riga.count()) ? String((await riga.first().locator('a').getAttribute('href')) === lotto) : NESSUNO;
  o['lista.vuoto'] = String(await page.locator('#ready-lot-list-empty').count());
  fa('riprendi');
  await page.locator('#ready-lot-goldsmith').selectOption({label: nome});
  await apri();
  await page.waitForURL(/\/ready-lots\/[0-9]+$/);
  await pronta(page, app);
  o['riprendi.stesso'] = String(percorso(page) === lotto);
  await lista();
  o['riprendi.righe'] = String(await page.locator('#ready-lot-list li', {hasText: nome}).count());
  fa('registro');
  o['registro.ready-lot-opened'] = String((await contaRegistro(page, base, 'ready_lot.opened')) - prima);
}

// oreficeriaPropria (slice 089, 090) aggiunge da «Anagrafiche» un'oreficeria di uno scenario che cambia i dati (le
// oreficerie non si cancellano), con un telefono se c'è.
async function oreficeriaPropria(page, base, app, nome, telefono) {
  await page.goto(`${base}/settings/anagrafiche`);
  await pronta(page, app);
  await page.locator('#new-oreficeria-name').fill(nome);
  if (telefono) await page.locator('#new-oreficeria-phone').fill(telefono);
  await page.locator('#new-oreficeria-form button[type="submit"]').click();
  await page.waitForFunction(x => !!document.body && document.body.innerText.includes(x), `Oreficeria "${nome}" aggiunta.`);
  await pronta(page, app);
}

// bustaPropria (slice 089, 090) crea da «Nuova riparazione» una busta dell'oreficeria nome: accettata il 05/06/2025, con
// un numero del 2025 (scritto in «N° Busta» dello standalone, numeroDel2025: in fondo alle liste, che gli altri scenari
// non guardano) e il QR dato. Con portala la mette «pronta» dalla scheda. Dà il numero letto dal messaggio (senza «N. »
// né «#») e l'id della scheda.
async function bustaPropria(page, base, app, {nome, cliente, descrizione, qr, numero25, portala}) {
  await page.goto(`${base}/nuova`);
  await pronta(page, app);
  await page.locator('[name="repair[customer_name]"]').fill(cliente);
  await page.locator('[name="repair[description]"]').fill(descrizione);
  await page.locator('[name="repair[accepted_on]"]').fill('2025-06-05');
  await page.locator('[name="repair[qr_code]"]').fill(qr);
  await page.locator('[name="repair[oreficeria_id]"]').selectOption({label: nome});
  await numeroDel2025(page, numero25);
  await page.locator('#new-repair-form button[type=submit]').click();
  await page.locator(SEL_INFO, {hasText: SALVATA}).first().waitFor();
  await pronta(page, app);
  const m = (await messaggio(page)).match(NUMERO_SALVATA) || [];
  const numero = (m[1] || m[2] || '').replace(/^N\. /, '').replace(/^#/, '');
  await cercaQR(page, base, app, qr, '/');
  await page.locator('#qr-btn-consulta').click();
  await page.waitForURL(/\/repairs\/[0-9]+/);
  await pronta(page, app);
  const id = Number(percorso(page).split('/').pop());
  if (portala) {
    await page.locator('#transition_status').selectOption('pronta');
    await page.locator('#repair-status-form button').click();
    await page.locator('#repair-notice').waitFor();
    await pronta(page, app);
  }
  return {numero, id};
}

// lotti-buste (slice 089): la pagina di un lotto d'avviso /ready-lots/L nelle due app (ORE-06, ORE-08, ORE-09), su **dati
// suoi** (regola degli scenari che cambiano i dati, SVILUPPO.md). Prima, con e senza dati, un lotto che non c'è (torna alla
// lista con «Lotto non trovato.»); sul database vuoto solo quello. Con i dati, per vista: un'oreficeria propria «Zz Banco 089 <vista>» con un telefono (le
// oreficerie non si cancellano: anagrafica-oreficerie sul telefono ne conta una in più, `pagina.righe` 7), cinque buste da
// «Nuova riparazione» di quell'oreficeria (cliente «Cliente lotto 089 <vista>», QR «SINT-LOTTO-089-<vista>-A…E», accettate
// il 05/06/2025: numeri del 2025, in fondo alle liste, scritti in «N° Busta» dello standalone con numeroDel2025, 8090-8094/25
// sul PC e 8095-8099/25 sul telefono; il numero non si confronta e nei testi diventa «#N»), A, B, C e D pronte, E in
// lavorazione; D avvisata singolarmente dalla scheda (popup, «{link}» tolto dal testo, D-74: si torna alla lista). Poi il
// lotto dell'oreficeria da /ready-lots e la sua pagina: intestazione, le due porte dell'aggiunta, «Nessuna busta.», le
// candidate (D, C, B, A: stessa data di pronta, id decrescenti; D segnata «Già avvisata…» con «Apri busta» e senza
// «Aggiungi»); A per numero scritto (quello che ognuna delle app ha dato), B per QR, C dalla candidata («✓ Aggiunta busta
// #N», il conto); A di nuovo («Busta già in questo lotto.», un esito); i rifiuti: D per QR («già avvisata»), E per QR
// («Busta non pronta.»), un numero che non c'è (il campo lo tiene), il campo vuoto; «Rimuovi» su A («Busta #N rimossa dal
// lotto.», A di nuovo fra le candidate); la scheda di B («In un lotto ancora aperto: …» con «Apri il lotto» verso il lotto).
// Le righe del registro si contano prima e dopo. I messaggi si leggono solo quando sono gli ultimi di quel tipo: nello
// standalone un flash resta finché un altro dello stesso tipo non lo sostituisce. Il QR scritto in «Aggiungi dal QR» dopo
// una riuscita non si guarda (lo standalone lo lascia nel campo, il Go ricarica la pagina). Il lotto resta aperto con B e
// C: nessun altro scenario legge le sue buste (lotti-apertura sul telefono vede la riga del lotto, non la confronta).
async function lottiBuste(page, {base, app, vista, dati}, o) {
  page.__app = app;
  const pc = vista === 'desktop';
  const attendi = async t => {
    await page.waitForFunction(x => !!document.body && document.body.innerText.includes(x), t);
    await pronta(page, app);
  };
  const conN = s => s.replace(/(#|N\. )[0-9]+\/[0-9]{2}/g, '#N');
  // Con e senza dati: le attese di «atteso» valgono anche con i dati (atteso_dati le completa, non le sostituisce).
  fa('vuoto');
  await page.goto(`${base}/ready-lots/999999`);
  await pronta(page, app);
  o['vuoto.percorso'] = percorso(page);
  o['vuoto.errore'] = await errore(page);
  if (!dati) return;
  const nome = `Zz Banco 089 ${vista}`;
  const lettere = ['A', 'B', 'C', 'D', 'E'];
  const codice = l => `SINT-LOTTO-089-${vista}-${l}`;
  const numero = {}, id = {};
  fa('oreficeria');
  await oreficeriaPropria(page, base, app, nome, pc ? '+39 000 089 0001' : '+39 000 089 0002');
  fa('buste');
  for (const [i, l] of lettere.entries()) {
    const b = await bustaPropria(page, base, app, {nome, cliente: `Cliente lotto 089 ${vista}`, descrizione: `Anello lotto 089 ${vista} ${l}`, qr: codice(l),
      numero25: `${(pc ? 8090 : 8095) + i}/25`, portala: l !== 'E'});
    numero[l] = b.numero;
    id[l] = b.id;
  }
  o['buste.numeri'] = String(lettere.filter(l => /^[0-9]+\/25$/.test(numero[l])).length);
  fa('avvisa');
  await apriScheda(page, base, app, id.D);
  await page.locator('#repair-notify').click();
  await page.locator('#notice-popup').waitFor();
  await page.locator('#notice-body').fill((await page.locator('#notice-body').inputValue()).replace('{link}', 'in negozio'));
  const prima = page.context().waMe.length;
  await Promise.all([page.waitForURL(u => new URL(u).pathname === '/', {timeout: 20000}), page.locator('#notice-send').click()]);
  await pronta(page, app);
  for (let i = 0; i < 100 && page.context().waMe.length === prima; i++) await page.waitForTimeout(100);
  o['avvisa.aperture'] = String(page.context().waMe.length - prima);
  for (const p of page.context().pages()) if (p !== page) await p.close().catch(() => {});
  const azioni = ['ready_lot.bag_added', 'ready_lot.bag_removed'];
  const registro = {};
  for (const a of azioni) registro[a] = await contaRegistro(page, base, a);
  fa('pagina');
  await page.goto(`${base}/ready-lots`);
  await pronta(page, app);
  await page.locator('#ready-lot-goldsmith').selectOption({label: nome});
  await page.locator('#ready-lot-open-form button[type="submit"]').click();
  await page.waitForURL(/\/ready-lots\/[0-9]+$/);
  await pronta(page, app);
  const lotto = percorso(page);
  const conta = async () => testo(await page.locator('#ready-lot-bag-count').innerText());
  const candidata = l => page.locator(`#ready-lot-candidate-${id[l]}`);
  const lettereCandidate = async () => (await page.locator('#ready-lot-candidates li').allInnerTexts())
    .map(t => (t.match(/lotto 089 \S+ ([A-E])/) || [0, '?'])[1]).join(' ') || NESSUNO;
  o['pagina.titolo'] = testo(await page.locator('h1').first().innerText());
  o['pagina.intestazione'] = testo(await page.locator('h1 + p').first().innerText()).replace(/#[0-9]+/, '#L').replace(DATA_ORA, '<data>');
  o['pagina.indietro'] = await page.locator('#ready-lot-back').getAttribute('href');
  o['pagina.badge'] = String(await page.locator('#ready-lot-closed-badge').count());
  o['pagina.aggiungi'] = testo(await page.locator('#ready-lot-add-form button[type="submit"]').innerText());
  o['pagina.qr'] = `${await page.locator('#ready-lot-scan-code').getAttribute('placeholder')} | ${testo(await page.locator('#ready-lot-scan-form button[type="submit"]').innerText())}`;
  o['pagina.buste'] = testo(await page.locator('#ready-lot-bags h2').innerText());
  o['pagina.vuoto'] = (await page.locator('#ready-lot-bags-empty').count()) ? testo(await page.locator('#ready-lot-bags-empty').innerText()) : NESSUNO;
  o['pagina.candidate-titolo'] = (await page.locator('#ready-lot-candidates h2').count()) ? testo(await page.locator('#ready-lot-candidates h2').innerText()) : NESSUNO;
  o['pagina.candidate'] = await lettereCandidate();
  o['pagina.avvisata'] = (await page.locator(`#ready-lot-candidate-${id.D}-told`).count()) ? testo(await page.locator(`#ready-lot-candidate-${id.D}-told`).innerText()) : NESSUNO;
  o['pagina.avvisata-link'] = (await candidata('D').locator('a').count()) ? `${testo(await candidata('D').locator('a').first().innerText())} ${String(await candidata('D').locator('a').first().getAttribute('href')).replace(/[0-9]+/, 'ID')}` : NESSUNO;
  o['pagina.avvisata-pulsanti'] = String(await candidata('D').locator('button').count());
  o['pagina.libera'] = (await candidata('A').locator('button').count()) ? testo(await candidata('A').locator('button').first().innerText()) : NESSUNO;
  fa('numero');
  await page.locator('#ready-lot-bag-number').fill(numero.A);
  await page.locator('#ready-lot-add-form button[type="submit"]').click();
  await page.waitForFunction(() => (document.getElementById('ready-lot-bag-count') || {}).textContent === '1');
  await attendi('✓ Aggiunta busta');
  o['numero.messaggio'] = conN(await messaggio(page));
  o['numero.campo'] = JSON.stringify(await page.locator('#ready-lot-bag-number').inputValue());
  o['numero.riga'] = (await page.locator(`#ready-lot-bag-${id.A}`).count()) ? conN(testo(await page.locator(`#ready-lot-bag-${id.A}`).innerText())) : NESSUNO;
  fa('qr');
  await page.locator('#ready-lot-scan-code').fill(codice('B'));
  await page.locator('#ready-lot-scan-form button[type="submit"]').click();
  await page.waitForFunction(() => (document.getElementById('ready-lot-bag-count') || {}).textContent === '2');
  await pronta(page, app);
  o['qr.messaggio'] = conN(await messaggio(page));
  fa('candidata');
  await candidata('C').locator('button').first().click();
  await page.waitForFunction(() => (document.getElementById('ready-lot-bag-count') || {}).textContent === '3');
  await pronta(page, app);
  o['candidata.messaggio'] = conN(await messaggio(page));
  o['candidata.candidate'] = await lettereCandidate();
  o['candidata.buste'] = (await page.locator('#ready-lot-bags li').allInnerTexts()).map(t => (t.match(/lotto 089 \S+ ([A-E])/) || [0, '?'])[1]).join(' ');
  fa('doppia');
  await page.locator('#ready-lot-bag-number').fill(numero.A);
  await page.locator('#ready-lot-add-form button[type="submit"]').click();
  await attendi('Busta già in questo lotto.');
  o['doppia.messaggio'] = await messaggio(page);
  o['doppia.errore'] = await errore(page);
  o['doppia.conto'] = await conta();
  fa('avvisata');
  await page.locator('#ready-lot-scan-code').fill(codice('D'));
  await page.locator('#ready-lot-scan-form button[type="submit"]').click();
  await attendi('Busta già avvisata singolarmente');
  o['avvisata.errore'] = await errore(page);
  o['avvisata.conto'] = await conta();
  o['avvisata.candidata'] = String(await candidata('D').count());
  fa('non-pronta');
  await page.locator('#ready-lot-scan-code').fill(codice('E'));
  await page.locator('#ready-lot-scan-form button[type="submit"]').click();
  await attendi('Busta non pronta.');
  o['non-pronta.errore'] = await errore(page);
  fa('sconosciuta');
  await page.locator('#ready-lot-bag-number').fill('9998/24');
  await page.locator('#ready-lot-add-form button[type="submit"]').click();
  await attendi('Riparazione non trovata.');
  o['sconosciuta.errore'] = await errore(page);
  o['sconosciuta.campo'] = JSON.stringify(await page.locator('#ready-lot-bag-number').inputValue());
  fa('vuota');
  await page.locator('#ready-lot-bag-number').fill('');
  await page.locator('#ready-lot-add-form button[type="submit"]').click();
  await attendi('Numero busta, QR o id richiesto.');
  o['vuota.errore'] = await errore(page);
  o['vuota.conto'] = await conta();
  fa('togli');
  await page.locator(`#ready-lot-bag-${id.A} button`).first().click();
  await page.waitForFunction(() => (document.getElementById('ready-lot-bag-count') || {}).textContent === '2');
  await attendi('rimossa dal lotto.');
  o['togli.messaggio'] = conN(await messaggio(page));
  o['togli.candidate'] = await lettereCandidate();
  o['togli.riga'] = String(await page.locator(`#ready-lot-bag-${id.A}`).count());
  fa('scheda');
  await apriScheda(page, base, app, id.B);
  const inLotto = page.locator('#repair-notice-in-lot');
  o['scheda.in-lotto'] = (await inLotto.count()) ? testo(await inLotto.innerText()) : NESSUNO;
  o['scheda.apri'] = (await inLotto.locator('a').count()) ? String((await inLotto.locator('a').first().getAttribute('href')) === lotto) : NESSUNO;
  o['scheda.da-avvisare'] = String(await page.locator('#repair-notice-pending').count());
  fa('registro');
  for (const a of azioni) o[`registro.${a.replace(/[._]/g, '-')}`] = String((await contaRegistro(page, base, a)) - registro[a]);
}

// lotti-invio (slice 091): «📤 Chiudi e invia» del lotto d'avviso nelle due app (ORE-10), su **dati suoi** (regola degli
// scenari che cambiano i dati, SVILUPPO.md). Sul database vuoto niente. Con i dati, per vista: un'oreficeria propria «Zz
// Banco 091 <vista>» con un telefono (le oreficerie non si cancellano: anagrafica-oreficerie sul telefono ne conta una in
// più, `pagina.righe` 8), tre buste da «Nuova riparazione» di quell'oreficeria, pronte (cliente «Cliente lotto 091 <vista>»,
// QR «SINT-LOTTO-091-<vista>-A…C», accettate il 05/06/2025: 8101-8103/25 sul PC e 8104-8106/25 sul telefono, scritti in «N°
// Busta» dello standalone con numeroDel2025; il numero non si confronta). Nel banco il Go non ha `indirizzo_pubblico`: un
// testo con {link} non parte (D-46 estesa), quindi lo scenario salva prima «Avviso lotto pronte oreficeria» e «Avviso
// oreficeria» senza {link} nelle due app e li rimette predefiniti alla fine, anche dopo un errore (come testi-invii e
// pannello-qr); il link pubblico del lotto si guarda nel riquadro, ridotto a «/lotto/<gettone>» (lo standalone scrive
// l'indirizzo intero, il Go senza indirizzo pubblico il solo percorso; il gettone è casuale).
//   - il lotto dell'oreficeria da /ready-lots con A e B dalle candidate; il riquadro «Chiudi e invia» (titolo, frase,
//     pulsante, la domanda di data-confirm, nessun avviso «senza numero»);
//   - una seconda scheda sullo stesso lotto; dalla prima A torna «In lavorazione», poi «📤 Chiudi e invia» nella seconda:
//     «Busta #ID: non è più pronta. Niente è partito.» nello standalone (l'id della busta diventa «#ID») e «Busta #N: …»
//     nel Go (D-99, Stefano 17/11/2026: il numero visibile della busta, «#0045/26» o «N. 3/25», diventa «#N»; riga in
//     bench/deviazioni.json); `rifiuto.busta` controlla che il testo nomini A (l'id o il numero); il lotto resta aperto;
//     A di nuovo «Pronta»;
//   - «📤 Chiudi e invia» nella prima (la domanda, accettata): il messaggio, il riquadro «Lotto inviato» (frase, il testo
//     del lotto, «Apri WhatsApp» con il numero dell'oreficeria e lo stesso testo, «👥 Scegli contatto» senza numero, il
//     link), la pagina in sola lettura (badge, niente aggiunte, candidate, pulsante); il conto delle buste;
//   - la seconda scheda, ancora col pulsante, di nuovo «📤 Chiudi e invia»: «Lotto già inviato.» e «Questo lotto è
//     partito il …»;
//   - «Apri WhatsApp» nella prima: wa.me (annotato, mai raggiunto) e la pagina alla lista (D-74, nelle due app; si guarda
//     il solo percorso, perché la lista ricordata può avere i filtri di un altro scenario); riaperto il lotto, «Questo
//     lotto è partito il <data>. Il link pubblico è …»;
//   - senza numero: il telefono dell'oreficeria tolto da «Anagrafiche», un lotto nuovo con C: l'avviso prima, dopo
//     l'invio niente «Apri WhatsApp», «👥 Scegli contatto» e l'avviso «Nessun numero WhatsApp …»; il tocco su «👥 Scegli
//     contatto» porta alla lista; la scheda di C dice «contatto scelto sul telefono».
// Le righe del registro (ready_lot.sent, ready_notice.sent, outgoing.handed_off) si contano prima e dopo. I due lotti
// restano inviati: nessun altro scenario legge le buste di questa oreficeria; lotti-apertura sul telefono conta solo le
// righe della sua oreficeria nei lotti aperti (un lotto inviato non c'è).
async function lottiInvio(page, {base, app, vista, dati}, o) {
  page.__app = app;
  page.__base = base;
  if (!dati) return;
  const pc = vista === 'desktop';
  const ctx = page.context();
  const attendi = async (p, t) => {
    await p.waitForFunction(x => !!document.body && document.body.innerText.includes(x), t);
    await pronta(p, app);
  };
  const nome = `Zz Banco 091 ${vista}`;
  const lettere = ['A', 'B', 'C'];
  const id = {}, numero = {};
  // La domanda di data-confirm, accettata, e il suo testo.
  let finestra = NESSUNO;
  const accetta = d => { finestra = `${d.type()}: ${d.message()}`; return d.accept(); };
  // Il link del lotto ridotto al percorso, con il gettone (24 byte, 32 caratteri url-safe) come «<gettone>».
  const gettone = s => String(s || '').replace(/^https?:\/\/[^/\s]+/, '').replace(/\/lotto\/[A-Za-z0-9_-]{32}(?![A-Za-z0-9_-])/g, '/lotto/<gettone>');
  const testi = {
    wa_oreficeria_lotto: 'Buongiorno {oref_nome},\nho {n_buste} riparazioni pronte da farvi ritirare.\nDettagli in negozio.\n\n{negozio}',
    wa_oreficeria: 'Ciao {oref_nome}, busta n. {numero} pronta.\n{negozio}',
  };
  const salvaTesto = async (famiglia, nuovo) => {
    await page.goto(`${base}/settings/templates`);
    await pronta(page, app);
    if (nuovo === null) {
      page.once('dialog', d => d.accept());
      await page.locator(`#template-restore-${famiglia}`).click();
      await page.waitForFunction(() => /Testo predefinito ripristinato/.test(document.body ? document.body.innerText : ""));
    } else {
      await page.locator(`#template-text-${famiglia}`).fill(nuovo);
      await page.locator(`#template-${famiglia} button:not([id])`).click();
      await page.waitForFunction(() => /Testo salvato/.test(document.body ? document.body.innerText : ""));
    }
    await pronta(page, app);
  };
  const apriLotto = async () => {
    await page.goto(`${base}/ready-lots`);
    await pronta(page, app);
    await page.locator('#ready-lot-goldsmith').selectOption({label: nome});
    await page.locator('#ready-lot-open-form button[type="submit"]').click();
    await page.waitForURL(/\/ready-lots\/[0-9]+$/);
    await pronta(page, app);
    return percorso(page);
  };
  const aggiungi = async (l, n) => {
    await page.locator(`#ready-lot-candidate-${id[l]} button`).first().click();
    await page.waitForFunction(x => (document.getElementById('ready-lot-bag-count') || {}).textContent === x, String(n));
    await pronta(page, app);
  };
  const stato = async (l, s, scritta) => {
    await apriScheda(page, base, app, id[l]);
    await page.locator('#transition_status').selectOption(s);
    await page.locator('#repair-status-form button').click();
    await page.locator('#repair-status', {hasText: scritta}).waitFor();
    await pronta(page, app);
  };
  // invia tocca «📤 Chiudi e invia» nella scheda p, accetta la domanda e aspetta il testo t.
  const invia = async (p, t) => {
    finestra = NESSUNO;
    p.once('dialog', accetta);
    await p.locator('#ready-lot-send-button').click();
    await attendi(p, t);
  };
  const quanti = async (p, sel) => String(await p.locator(sel).count());
  const leggiSe = async (p, sel) => ((await p.locator(sel).count()) ? testo(await p.locator(sel).first().innerText()) : NESSUNO);
  const waMe = href => {
    if (!href) return NESSUNO;
    const u = new URL(href);
    return `${u.origin}/${decodeURIComponent(u.pathname.slice(1)) || '(vuoto)'} | ${JSON.stringify(u.searchParams.get('text') || '')}`;
  };
  const azioni = ['ready_lot.sent', 'ready_notice.sent', 'outgoing.handed_off'];
  const registro = {};
  let seconda = null;
  const salvati = [];
  try {
    fa('oreficeria');
    await oreficeriaPropria(page, base, app, nome, pc ? '+39 000 091 0001' : '+39 000 091 0002');
    fa('buste');
    for (const [i, l] of lettere.entries()) {
      const b = await bustaPropria(page, base, app, {nome, cliente: `Cliente lotto 091 ${vista}`, descrizione: `Anello lotto 091 ${vista} ${l}`,
        qr: `SINT-LOTTO-091-${vista}-${l}`, numero25: `${(pc ? 8101 : 8104) + i}/25`, portala: true});
      id[l] = b.id;
      numero[l] = b.numero;
    }
    fa('testi');
    for (const [f, t] of Object.entries(testi)) {
      salvati.push(f);
      await salvaTesto(f, t);
    }
    for (const a of azioni) registro[a] = await contaRegistro(page, base, a);
    fa('prima');
    const lotto = await apriLotto();
    await aggiungi('A', 1);
    await aggiungi('B', 2);
    o['prima.titolo'] = await leggiSe(page, '#ready-lot-send h2');
    o['prima.frase'] = await leggiSe(page, '#ready-lot-send p');
    o['prima.pulsante'] = await leggiSe(page, '#ready-lot-send-button');
    o['prima.domanda'] = String(await page.locator('#ready-lot-send-button').getAttribute('data-confirm'));
    o['prima.senza-numero'] = await quanti(page, '#ready-lot-send-no-number');
    fa('rifiuto');
    seconda = await ctx.newPage();
    await seconda.goto(`${base}${lotto}`);
    await pronta(seconda, app);
    await stato('A', 'in_lavorazione', 'In lavorazione');
    await invia(seconda, 'Niente è partito.');
    o['rifiuto.finestra'] = finestra;
    const rifiuto = await errore(seconda);
    o['rifiuto.errore'] = rifiuto.replace(/(#|N\. )[0-9]+\/[0-9]{2}/, '#N').replace(/#[0-9]+:/, '#ID:');
    o['rifiuto.busta'] = String(rifiuto.includes(`#${id.A}:`) || rifiuto.includes(`${numero.A}:`));
    o['rifiuto.badge'] = await quanti(seconda, '#ready-lot-closed-badge');
    o['rifiuto.pulsante'] = await quanti(seconda, '#ready-lot-send-button');
    o['rifiuto.inviato'] = await quanti(seconda, '#ready-lot-sent');
    await stato('A', 'pronta', 'Pronta');
    fa('invia');
    await page.goto(`${base}${lotto}`);
    await pronta(page, app);
    await invia(page, 'Lotto inviato:');
    await page.locator('#ready-lot-sent').waitFor();
    o['invia.finestra'] = finestra;
    o['invia.messaggio'] = await messaggio(page);
    o['invia.errore'] = await errore(page);
    o['invia.titolo'] = await leggiSe(page, '#ready-lot-sent h2');
    o['invia.frase'] = await leggiSe(page, '#ready-lot-sent p');
    o['invia.testo'] = JSON.stringify(await page.locator('#ready-lot-sent-message').innerText());
    const wa = page.locator('#ready-lot-sent-whatsapp');
    o['invia.whatsapp'] = (await wa.count()) ? `${testo(await wa.innerText())} | ${await wa.getAttribute('target')} | ${waMe(await wa.getAttribute('href'))}` : NESSUNO;
    const scegli = page.locator('#ready-lot-sent-choose');
    o['invia.scegli'] = (await scegli.count()) ? `${testo(await scegli.innerText())} | ${await scegli.getAttribute('target')} | ${waMe(await scegli.getAttribute('href'))}` : NESSUNO;
    o['invia.stesso-testo'] = String((await wa.count()) && (await scegli.count()) &&
      new URL(await wa.getAttribute('href')).searchParams.get('text') === await page.locator('#ready-lot-sent-message').innerText());
    o['invia.senza-numero'] = await quanti(page, '#ready-lot-sent-no-number');
    o['invia.link'] = (await page.locator('#ready-lot-sent-link').count())
      ? `${gettone(testo(await page.locator('#ready-lot-sent-link').innerText()))} | ${gettone(await page.locator('#ready-lot-sent-link').getAttribute('href'))}` : NESSUNO;
    o['invia.badge'] = await quanti(page, '#ready-lot-closed-badge');
    o['invia.aggiunte'] = await quanti(page, '#ready-lot-add');
    o['invia.candidate'] = await quanti(page, '#ready-lot-candidates');
    o['invia.pulsante'] = await quanti(page, '#ready-lot-send-button');
    o['invia.partito'] = await quanti(page, '#ready-lot-gone');
    o['invia.buste'] = await leggiSe(page, '#ready-lot-bag-count');
    fa('gia-inviato');
    await invia(seconda, 'Lotto già inviato.');
    o['gia-inviato.errore'] = await errore(seconda);
    o['gia-inviato.partito'] = await quanti(seconda, '#ready-lot-gone');
    o['gia-inviato.pulsante'] = await quanti(seconda, '#ready-lot-send-button');
    o['gia-inviato.inviato'] = await quanti(seconda, '#ready-lot-sent');
    await seconda.close().catch(() => {});
    seconda = null;
    fa('apri');
    let prima = ctx.waMe.length;
    await Promise.all([page.waitForURL(u => new URL(u).pathname === '/', {timeout: 20000}), wa.click()]);
    await pronta(page, app);
    for (let i = 0; i < 100 && ctx.waMe.length === prima; i++) await page.waitForTimeout(100);
    o['apri.dopo'] = percorso(page);
    o['apri.aperture'] = String(ctx.waMe.length - prima);
    for (const p of ctx.pages()) if (p !== page) await p.close().catch(() => {});
    fa('dopo');
    await page.goto(`${base}${lotto}`);
    await pronta(page, app);
    o['dopo.partito'] = (await page.locator('#ready-lot-gone').count())
      ? gettone(testo(await page.locator('#ready-lot-gone').innerText()).replace(DATA_ORA, '<data>').replace(/https?:\/\/[^/\s]+\/lotto\//g, '/lotto/')).replace(/\s+\./g, '.') : NESSUNO;
    o['dopo.inviato'] = await quanti(page, '#ready-lot-sent');
    o['dopo.pulsante'] = await quanti(page, '#ready-lot-send-button');
    fa('senza-numero');
    await page.goto(`${base}/settings/anagrafiche`);
    await pronta(page, app);
    const righe = page.locator('#oreficerie-list > div[id^="oreficerie-"]');
    let mia = null;
    for (let i = 0; i < await righe.count(); i++) {
      if (await righe.nth(i).locator('input[name="oreficeria[name]"]').inputValue() === nome) mia = righe.nth(i);
    }
    await mia.locator('input[name="oreficeria[phone]"]').fill('');
    await mia.locator('button[type="submit"]').click();
    await attendi(page, 'Oreficeria aggiornata.');
    const altro = await apriLotto();
    o['senza-numero.nuovo'] = String(altro !== lotto);
    await aggiungi('C', 1);
    o['senza-numero.prima'] = await leggiSe(page, '#ready-lot-send-no-number');
    await invia(page, 'Lotto inviato:');
    await page.locator('#ready-lot-sent').waitFor();
    o['senza-numero.messaggio'] = await messaggio(page);
    o['senza-numero.whatsapp'] = await quanti(page, '#ready-lot-sent-whatsapp');
    o['senza-numero.scegli'] = (await scegli.count()) ? `${testo(await scegli.innerText())} | ${waMe(await scegli.getAttribute('href'))}` : NESSUNO;
    o['senza-numero.dopo'] = await leggiSe(page, '#ready-lot-sent-no-number');
    prima = ctx.waMe.length;
    await Promise.all([page.waitForURL(u => new URL(u).pathname === '/', {timeout: 20000}), scegli.click()]);
    await pronta(page, app);
    for (let i = 0; i < 100 && ctx.waMe.length === prima; i++) await page.waitForTimeout(100);
    o['senza-numero.apri'] = percorso(page);
    o['senza-numero.aperture'] = String(ctx.waMe.length - prima);
    for (const p of ctx.pages()) if (p !== page) await p.close().catch(() => {});
    await apriScheda(page, base, app, id.C);
    const invii = page.locator('#repair-send-history');
    o['senza-numero.scheda'] = (await invii.count()) ? String((await invii.textContent()).includes('contatto scelto sul telefono')) : NESSUNO;
    fa('registro');
    for (const a of azioni) o[`registro.${a.replace(/[._]/g, '-')}`] = String((await contaRegistro(page, base, a)) - registro[a]);
  } finally {
    // I testi di prima, anche dopo un errore; la fase dell'errore resta quella in cui è successo.
    const dove = fase;
    if (seconda) await seconda.close().catch(() => {});
    // Un ripristino fallito non si nasconde: fine.ripristino (atteso «ok») nomina i testi rimasti cambiati, che
    // testi-invii e pannello-qr troverebbero diversi (review della 091).
    const falliti = [];
    for (const f of salvati) {
      try {
        await salvaTesto(f, null);
      } catch {
        falliti.push(f);
      }
    }
    if (salvati.length) o['fine.ripristino'] = falliti.length ? `non ripristinati: ${falliti.join(' ')}` : 'ok';
    fa(dove);
  }
}

// lotti-reinvio (slice 093): lo storico dei lotti inviati, il dettaglio e il reinvio nelle due app (ORE-11, ORE-12,
// ORE-13). **Riusa i lotti della 091** (eccezione ai dati propri, come consegne-solleciti della 083): un lotto si apre e
// si invia solo con «📤 Chiudi e invia», quindi lotti-invio, che gira subito prima nella stessa vista (ordine dei nomi),
// lascia per vista due lotti inviati e non completati dell'oreficeria «Zz Banco 091 <vista>», con il telefono tolto alla
// fine; nessun altro scenario legge quei lotti né quell'oreficeria. Sul database vuoto niente. Con i dati, per vista:
//   - il testo «Avviso lotto pronte oreficeria» salvato senza {link} (nel banco il Go non ha `indirizzo_pubblico`: un
//     testo con {link} non si prepara, D-46 estesa) e rimesso predefinito alla fine, anche dopo un errore;
//   - lo storico: le righe dell'oreficeria (2), la più vecchia (il primo lotto della 091, con A e B: «2 buste», «2 da
//     ritirare»; id e data diventano «#ID» e «<data>»), «Apri»;
//   - il dettaglio: titolo, «Lotto #ID · inviato <data>», il link ridotto a «/lotto/<gettone>», le buste e «Apri busta»,
//     il riquadro «Reinvia all'oreficeria» senza testo;
//   - «Prepara di nuovo il messaggio» senza numero (la 091 l'ha tolto): il messaggio, il testo con 2 buste, solo «👥
//     Scegli contatto», l'avviso «Nessun numero WhatsApp …», «Pronto, non ancora registrato…»; nessuna riga scritta;
//   - il tocco su «👥 Scegli contatto»: WhatsApp e la pagina alla lista (D-74), una riga ready_lot.resent;
//   - il telefono rimesso all'oreficeria («+39 000 093 000N»), di nuovo «Prepara»: «↗ Apri WhatsApp» c'è; un doppio tocco
//     (dblclick) su «Apri WhatsApp» registra **una** riga (nello standalone la seconda apertura trova il messaggio già
//     registrato, nel Go il modulo è già partito; quante schede WhatsApp si aprono non si confronta);
//   - alla fine il telefono tolto di nuovo (com'era dopo la 091) e il testo predefinito.
// Le righe del registro ready_lot.resent e outgoing.handed_off si contano prima e dopo (2 e 2).
async function lottiReinvio(page, {base, app, vista, dati}, o) {
  page.__app = app;
  page.__base = base;
  if (!dati) return;
  const nome = `Zz Banco 091 ${vista}`;
  const telefono = vista === 'desktop' ? '+39 000 093 0001' : '+39 000 093 0002';
  const attendi = async t => {
    await page.waitForFunction(x => !!document.body && document.body.innerText.includes(x), t);
    await pronta(page, app);
  };
  const gettone = s => String(s || '').replace(/^https?:\/\/[^/\s]+/, '').replace(/\/lotto\/[A-Za-z0-9_-]{32}(?![A-Za-z0-9_-])/g, '/lotto/<gettone>');
  const salvaTesto = async nuovo => {
    await page.goto(`${base}/settings/templates`);
    await pronta(page, app);
    if (nuovo === null) {
      page.once('dialog', d => d.accept());
      await page.locator('#template-restore-wa_oreficeria_lotto').click();
      await page.waitForFunction(() => /Testo predefinito ripristinato/.test(document.body ? document.body.innerText : ""));
    } else {
      await page.locator('#template-text-wa_oreficeria_lotto').fill(nuovo);
      await page.locator('#template-wa_oreficeria_lotto button:not([id])').click();
      await page.waitForFunction(() => /Testo salvato/.test(document.body ? document.body.innerText : ""));
    }
    await pronta(page, app);
  };
  const salvaTelefono = async t => {
    await page.goto(`${base}/settings/anagrafiche`);
    await pronta(page, app);
    const righe = page.locator('#oreficerie-list > div[id^="oreficerie-"]');
    let mia = null;
    for (let i = 0; i < await righe.count(); i++) {
      if (await righe.nth(i).locator('input[name="oreficeria[name]"]').inputValue() === nome) mia = righe.nth(i);
    }
    await mia.locator('input[name="oreficeria[phone]"]').fill(t);
    await mia.locator('button[type="submit"]').click();
    await attendi('Oreficeria aggiornata.');
  };
  const quanti = async sel => String(await page.locator(sel).count());
  const leggiSe = async sel => ((await page.locator(sel).count()) ? testo(await page.locator(sel).first().innerText()) : NESSUNO);
  const senzaId = s => s.replace(/#[0-9]+\b/g, '#ID').replace(DATA_ORA, '<data>');
  const prepara = async () => {
    await page.locator('#ready-lot-resend-prepare').click();
    await page.locator('#ready-lot-resend-message').waitFor();
    await pronta(page, app);
  };
  const azioni = ['ready_lot.resent', 'outgoing.handed_off'];
  const registro = {};
  let testoCambiato = false, telefonoCambiato = false;
  try {
    fa('testi');
    testoCambiato = true;
    await salvaTesto('Buongiorno {oref_nome},\nho {n_buste} riparazioni pronte da farvi ritirare.\nDettagli in negozio.\n\n{negozio}');
    for (const a of azioni) registro[a] = await contaRegistro(page, base, a);
    fa('storico');
    await page.goto(`${base}/ready-lots`);
    await pronta(page, app);
    // Sulla lista il link allo storico non ha id (nello standalone ready-lot-history-link è il link pubblico del dettaglio).
    await page.locator('a[href="/ready-lots/history"]', {hasText: 'Storico dei lotti inviati'}).click();
    await page.waitForURL(u => new URL(u).pathname === '/ready-lots/history');
    await pronta(page, app);
    o['storico.titolo'] = testo(await page.locator('h1').first().innerText());
    o['storico.indietro'] = String(await page.locator('#ready-lot-history-back').getAttribute('href'));
    const righe = page.locator('li[id^="ready-lot-history-"]').filter({has: page.locator('strong', {hasText: nome})});
    o['storico.righe'] = String(await righe.count());
    const vecchia = righe.last();
    o['storico.riga'] = senzaId(testo(await vecchia.innerText()));
    o['storico.da-ritirare'] = testo(await vecchia.locator('[id$="-open"]').innerText());
    o['storico.completato'] = String(await vecchia.locator('[id$="-closed"]').count());
    await vecchia.locator('a').first().click();
    await page.waitForURL(/\/ready-lots\/history\/[0-9]+$/);
    await pronta(page, app);
    const dettaglio = percorso(page);
    fa('dettaglio');
    o['dettaglio.titolo'] = testo(await page.locator('h1').first().innerText());
    o['dettaglio.indietro'] = String(await page.locator('#ready-lot-history-back').getAttribute('href'));
    o['dettaglio.link'] = (await page.locator('#ready-lot-history-link').count())
      ? `${gettone(testo(await page.locator('#ready-lot-history-link').innerText()))} | ${gettone(await page.locator('#ready-lot-history-link').getAttribute('href'))}` : NESSUNO;
    o['dettaglio.buste'] = await quanti('li[id^="ready-lot-history-bag-"]');
    o['dettaglio.ritirate'] = await quanti('[id^="ready-lot-history-bag-"][id$="-collected"]');
    o['dettaglio.apri-busta'] = String(/^\/repairs\/[0-9]+$/.test(await page.locator('li[id^="ready-lot-history-bag-"] a').first().getAttribute('href') || ''));
    o['dettaglio.reinvio'] = await leggiSe('#ready-lot-resend h2');
    o['dettaglio.frase'] = await leggiSe('#ready-lot-resend p');
    o['dettaglio.prepara'] = await leggiSe('#ready-lot-resend-prepare');
    o['dettaglio.testo'] = await quanti('#ready-lot-resend-message');
    fa('prepara');
    await prepara();
    o['prepara.messaggio'] = await messaggio(page);
    o['prepara.errore'] = await errore(page);
    o['prepara.testo'] = JSON.stringify(await page.locator('#ready-lot-resend-message').innerText());
    o['prepara.whatsapp'] = await quanti('#ready-lot-resend-opening');
    o['prepara.scegli'] = await leggiSe('#ready-lot-resend-choose');
    o['prepara.senza-numero'] = await leggiSe('#ready-lot-resend-no-number');
    o['prepara.in-attesa'] = await leggiSe('#ready-lot-resend-pending');
    o['prepara.registro'] = String((await contaRegistro(page, base, 'ready_lot.resent')) - registro['ready_lot.resent']);
    await page.goto(`${base}${dettaglio}`);
    await pronta(page, app);
    await prepara();
    fa('scegli');
    const prima = page.context().waMe.length;
    await Promise.all([page.waitForURL(u => new URL(u).pathname === '/', {timeout: 20000}), page.locator('#ready-lot-resend-choose').click()]);
    await pronta(page, app);
    for (let i = 0; i < 100 && page.context().waMe.length === prima; i++) await page.waitForTimeout(100);
    o['scegli.dopo'] = percorso(page);
    o['scegli.aperture'] = String(page.context().waMe.length - prima);
    const ultimo = page.context().waMe[page.context().waMe.length - 1] || '';
    o['scegli.wa'] = ultimo ? `${new URL(ultimo).pathname} | ${JSON.stringify(new URL(ultimo).searchParams.get('text') || '')}` : NESSUNO;
    for (const p of page.context().pages()) if (p !== page) await p.close().catch(() => {});
    o['scegli.registro'] = String((await contaRegistro(page, base, 'ready_lot.resent')) - registro['ready_lot.resent']);
    fa('numero');
    telefonoCambiato = true;
    await salvaTelefono(telefono);
    await page.goto(`${base}${dettaglio}`);
    await pronta(page, app);
    await prepara();
    o['numero.whatsapp'] = await leggiSe('#ready-lot-resend-opening');
    o['numero.senza-numero'] = await quanti('#ready-lot-resend-no-number');
    fa('doppio');
    await Promise.all([page.waitForURL(u => new URL(u).pathname === '/', {timeout: 20000}), page.locator('#ready-lot-resend-opening').dblclick()]);
    await pronta(page, app);
    for (const p of page.context().pages()) if (p !== page) await p.close().catch(() => {});
    o['doppio.dopo'] = percorso(page);
    fa('registro');
    for (const a of azioni) o[`registro.${a.replace(/[._]/g, '-')}`] = String((await contaRegistro(page, base, a)) - registro[a]);
  } finally {
    // Il telefono (com'era dopo la 091: nessuno) e il testo di prima, anche dopo un errore; la fase dell'errore resta
    // quella in cui è successo. Un ripristino fallito non si nasconde (fine.ripristino, atteso «ok»).
    const dove = fase;
    const falliti = [];
    if (telefonoCambiato) {
      try {
        await salvaTelefono('');
      } catch {
        falliti.push('telefono');
      }
    }
    if (testoCambiato) {
      try {
        await salvaTesto(null);
      } catch {
        falliti.push('wa_oreficeria_lotto');
      }
      o['fine.ripristino'] = falliti.length ? `non ripristinati: ${falliti.join(' ')}` : 'ok';
    }
    fa(dove);
  }
}

// profilo-negozio (slice 087): la sezione «Profilo negozio» di /settings/shop (ADM-02), nelle due app. La pagina dello standalone
// ha anche le sezioni di ADM-03…07 (operatività, stima, logo, colori, accessi): qui si guarda solo il modulo #shop-form. Sul
// database vuoto solo la pagina (senza un profilo, salvare ne crea uno e non lo si può rimettere). Con i dati il profilo c'è
// («Negozio» e la garanzia predefinita dell'import) e lo scenario lo CAMBIA: legge i 17 campi come sono, poi per vista prova un
// rifiuto con nome e garanzia di soli spazi (due «Campo obbligatorio.», niente salvato), un campo oltre il limite (orari, 501
// caratteri), un codice fiscale che non lo è, un salvataggio buono con spazi e codice fiscale da normalizzare (messaggio, campi
// riletti dopo il ricaricamento), le due pagine che leggono il profilo (/privacy: titolo e riquadro del titolare; la home: il
// nome) e ALLA FINE RIMETTE IL PROFILO COM'ERA (gli stessi 17 valori riscritti dal modulo, riletti dopo il ricaricamento): gli
// altri scenari leggono il profilo (messaggi WhatsApp con {negozio} e {citta}, PDF, intestazioni). Se un passo cade, il
// ripristino gira lo stesso (finally). Le righe del registro shop_profile.updated si contano prima e dopo: due per vista (il
// salvataggio e il ripristino; i rifiuti non scrivono). Nessun doppio tocco su «Salva impostazioni».
const CAMPI_NEGOZIO = ['shop_name', 'subtitle', 'legal_name', 'fiscal_id', 'tax_code', 'address', 'postal_code', 'city', 'province',
  'phone', 'email', 'sales_email', 'opening_hours', 'instagram', 'facebook', 'website', 'warranty_terms'];

async function profiloNegozio(page, {base, app, vista, dati}, o) {
  page.__app = app;
  const pagina = async () => {
    await page.goto(`${base}/settings/shop`);
    await pronta(page, app);
  };
  const campo = k => page.locator(`#shop_profile_${k}`);
  const invia = () => page.locator('#shop-form button').click();
  const attendi = async t => {
    await page.waitForFunction(x => !!document.body && document.body.innerText.includes(x), t);
    await pronta(page, app);
  };
  const leggiCampi = async () => {
    const v = {};
    for (const k of CAMPI_NEGOZIO) v[k] = await campo(k).inputValue();
    return v;
  };
  const azione = 'shop_profile.updated';
  // Il registro si legge e si aspetta che la sua pagina sia collegata: la lettura (e la riga audit.viewed) dello standalone
  // finisce prima di cambiare pagina o di chiudere.
  const registro = async () => {
    const n = await contaRegistro(page, base, azione);
    await pronta(page, app);
    return n;
  };
  const sotto = async () => (await page.locator('#shop-form p').allInnerTexts()).map(testo).filter(Boolean).join(' | ') || NESSUNO;
  fa('pagina');
  await pagina();
  o['pagina.titolo'] = testo(await page.locator('h1').first().innerText());
  o['pagina.scheda'] = `${testo(await page.locator('#settings-tab-shop').innerText())} (${await page.locator('#settings-tab-shop').getAttribute('aria-current')})`;
  o['pagina.sezione'] = testo(await page.getByRole('heading', {name: 'Profilo negozio'}).innerText());
  o['pagina.etichette'] = (await page.locator('#shop-form label > span.label').allInnerTexts()).map(testo).join(' | ');
  o['pagina.campi'] = (await page.locator('#shop-form [name^="shop_profile["]').evaluateAll(els => els.map(e =>
    `${e.name}:${e.tagName === 'TEXTAREA' ? 'textarea' : e.type}${e.required ? '*' : ''}${e.autocomplete === 'off' ? ' off' : ''}`))).join(' | ');
  o['pagina.pulsante'] = testo(await page.locator('#shop-form button').innerText());
  if (!dati) return;
  const prima = await registro();
  fa('originale');
  await pagina();
  const orig = await leggiCampi();
  o['pagina.nome'] = orig.shop_name;
  o['pagina.garanzia'] = testo(orig.warranty_terms).slice(0, 40);
  let ripristinato = false;
  let erroreRipristino = NESSUNO;
  const ripristina = async () => {
    await pagina();
    for (const k of CAMPI_NEGOZIO) await campo(k).fill(orig[k]);
    await invia();
    await attendi('Impostazioni salvate. I documenti emessi restano invariati.');
    ripristinato = true;
  };
  const nome = `Bottega Banco 087 ${vista}`;
  try {
    fa('rifiuto');
    await campo('shop_name').fill('   ');
    await campo('warranty_terms').fill('   ');
    await campo('city').fill('  Paese Banco  ');
    await invia();
    await attendi('Campo obbligatorio.');
    o['rifiuto.errori'] = await sotto();
    o['rifiuto.messaggio'] = await messaggio(page);
    o['rifiuto.errore-alto'] = await errore(page);
    o['rifiuto.campo-nome'] = JSON.stringify(await campo('shop_name').inputValue());
    o['rifiuto.campo-citta'] = JSON.stringify(await campo('city').inputValue());
    o['rifiuto.campo-garanzia'] = JSON.stringify(await campo('warranty_terms').inputValue());
    await pagina();
    o['rifiuto.salvato'] = String((await campo('shop_name').inputValue()) === orig.shop_name);
    fa('lungo');
    await campo('opening_hours').fill('a'.repeat(501));
    await invia();
    await attendi('Troppo lungo (massimo 500 caratteri).');
    o['lungo.errori'] = await sotto();
    o['lungo.lunghezza'] = String((await campo('opening_hours').inputValue()).length);
    fa('codice-fiscale');
    await pagina();
    await campo('tax_code').fill('ABC123');
    await invia();
    await attendi('codice fiscale non valido');
    o['cf.errori'] = await sotto();
    o['cf.campo'] = JSON.stringify(await campo('tax_code').inputValue());
    fa('salva');
    await pagina();
    const nuovi = {shop_name: `  ${nome}  `, subtitle: ' Sottotitolo 087 ', legal_name: 'Banco 087 S.r.l.', fiscal_id: '00000008701',
      tax_code: ' zzz bnc 80a01 a000z ', address: 'Via Banco 87', postal_code: '00087', city: 'Paese Banco', province: 'BX',
      phone: '0423 000 087', email: 'banco087@example.test', sales_email: 'vendite087@example.test',
      opening_hours: 'Lun-Sab 9-19 (banco 087)', instagram: '@banco087', facebook: 'banco087.fb', website: 'banco087.example.test',
      warranty_terms: 'Prima riga 087.\n\nSeconda riga 087.'};
    for (const k of CAMPI_NEGOZIO) await campo(k).fill(nuovi[k]);
    await invia();
    await attendi('Impostazioni salvate. I documenti emessi restano invariati.');
    o['salva.messaggio'] = await messaggio(page);
    o['salva.errore'] = await errore(page);
    await pagina();
    const riletti = await leggiCampi();
    o['salva.campi'] = JSON.stringify(riletti);
    fa('legge');
    await page.goto(`${base}/privacy`);
    await page.waitForLoadState('load');
    o['legge.privacy-titolo'] = await page.title();
    o['legge.privacy-titolare'] = testo(await page.locator('#privacy-titolare').innerText());
    await page.goto(`${base}/`);
    await pronta(page, app);
    o['legge.home-nome'] = String((await page.locator('body').innerText()).includes(nome));
  } finally {
    if (!ripristinato) await ripristina().catch(e => { erroreRipristino = String(e && e.message || e).split('\n')[0].replace(/https:\/\/[^\s/]+/g, '<app>'); });
  }
  fa('ripristino');
  await pagina();
  const finali = await leggiCampi();
  o['ripristino.errore'] = erroreRipristino;
  o['ripristino.uguale'] = String(CAMPI_NEGOZIO.every(k => finali[k] === orig[k]));
  o['ripristino.nome'] = finali.shop_name;
  fa('registro');
  o['registro.shop-profile-updated'] = String((await registro()) - prima);
}

// stima-negozio (slice 102): la sezione «Stima tempi» di /settings/shop (ADM-04, RIP-H-55), nelle due app. Sul database vuoto
// solo la pagina (senza dati la stima non ha buste da leggere e non c'è una scheda da guardare). Con i dati l'interruttore e i
// giorni sono quelli di partenza (spenta, 7-8: nessun import li accende) e lo scenario li CAMBIA: per vista prova cinque
// salvataggi (giorni fuori campo -5 e 120, il massimo sotto il minimo, un testo che non è un intero, due campi vuoti, la stima
// spenta con 10-12), poi la stima ACCESA con 10-12 e, con la stima accesa, la scheda della busta 10 (in preventivo, senza
// consegna: «Dati insufficienti» e nessun «non mostrata ai clienti» perché il cliente la vedrebbe) e la 5; infine RIMETTE
// L'INTERRUTTORE E I GIORNI COME ERANO (spenta, 7-8). Fuori dallo scenario nessuno cambia la stima: la leggono le pagine /r/
// e /stato (061) e la scheda (101); gli scenari HTTP girano prima e li guarda scheda-stima con la stima spenta. Se un passo
// cade, il ripristino gira lo stesso (finally). Le righe del registro settings.estimate_updated si contano prima e dopo:
// sette per vista (i sette salvataggi, il ripristino compreso). Il browser rifiuta i giorni fuori campo («min» e «max» del
// campo): il modulo si invia lo stesso con la convalida spenta (noValidate), perché a decidere è il server; il testo che non è
// un intero si scrive dopo aver cambiato il tipo del campo in «text» (un campo numerico non lo accetta).
async function stimaNegozio(page, {base, app, vista, dati}, o) {
  page.__app = app;
  const pagina = async () => {
    await page.goto(`${base}/settings/shop`);
    await pronta(page, app);
  };
  const campo = k => page.locator(`#estimate_${k}`);
  const invia = () => page.locator('#estimate-form button').click();
  const attendi = async t => {
    await page.waitForFunction(x => !!document.body && document.body.innerText.includes(x), t);
    await pronta(page, app);
  };
  // «Oggi» con i conti al posto dei numeri (dipendono dai dati: la catena ha meno di 20 buste pronte, quindi «Dati insufficienti»).
  const oggi = async () => testo(await page.locator('#estimate-today').innerText()).replace(/\(ora \d+\)/, '(ora N)')
    .replace(/\d+ buste in bottega, \d+ passate/, 'N buste in bottega, M passate');
  const leggiStima = async () => JSON.stringify({acceso: await campo('enabled').isChecked(), min: await campo('artisan_min').inputValue(),
    max: await campo('artisan_max').inputValue()});
  const registro = async () => {
    const n = await contaRegistro(page, base, 'settings.estimate_updated');
    await pronta(page, app);
    return n;
  };
  // salva scrive il modulo (acceso, min, max come testo) senza la convalida del browser e dice il messaggio dopo l'invio.
  const salva = async (acceso, min, max, comeTesto) => {
    await pagina();
    await page.locator('#estimate-form').evaluate((f, testo) => {
      f.noValidate = true;
      if (testo) for (const id of ['estimate_artisan_min', 'estimate_artisan_max']) document.getElementById(id).type = 'text';
    }, comeTesto);
    await campo('enabled').setChecked(acceso);
    await campo('artisan_min').fill(min);
    await campo('artisan_max').fill(max);
    await invia();
    await attendi('Stima tempi salvata.');
    return messaggio(page);
  };
  // La scheda con la stima: il riquadro come testo (con «ora N» al posto del numero, che dipende dai dati) e se dice che il
  // cliente non la vede.
  const scheda = async (id, k) => {
    await apriScheda(page, base, app, id);
    const riquadro = page.locator('#repair-time-estimate');
    if (!(await riquadro.count())) { o[`${k}.riquadro`] = NESSUNO; return; }
    const t = testo(await riquadro.innerText());
    o[`${k}.riquadro`] = t.replace(/\(ora \d+\)/, '(ora N)').replace(/ su \d+ buste/, ' su N buste').replace(/Giorni in più: \d+/, 'Giorni in più: N');
    o[`${k}.non-mostrata`] = String(t.includes('non mostrata ai clienti'));
  };
  fa('pagina');
  await pagina();
  o['pagina.titolo'] = testo(await page.locator('h1').first().innerText());
  o['pagina.sezione'] = testo(await page.locator('#shop-estimate h2').innerText());
  o['pagina.frase'] = testo(await page.locator('#shop-estimate p').first().innerText());
  o['pagina.oggi'] = await oggi();
  o['pagina.etichette'] = (await page.locator('#estimate-form label').allInnerTexts()).map(testo).join(' | ');
  o['pagina.campi'] = (await page.locator('#estimate-form [name^="estimate["]').evaluateAll(els => els.map(e =>
    `${e.name}:${e.type}${e.type === 'number' ? ` ${e.min}-${e.max}` : ''}`))).join(' | ');
  o['pagina.pulsante'] = testo(await page.locator('#estimate-form button').innerText());
  if (!dati) return;
  const prima = await registro();
  fa('originale');
  await pagina();
  o['originale.campi'] = await leggiStima();
  const orig = JSON.parse(o['originale.campi']);
  await scheda(10, 'originale.scheda10');
  let ripristinato = false;
  let erroreRipristino = NESSUNO;
  const ripristina = async () => {
    await salva(orig.acceso, orig.min, orig.max, false);
    ripristinato = true;
  };
  try {
    for (const [nome, acceso, min, max, comeTesto] of [['fuori', true, '-5', '120', false], ['invertiti', true, '30', '10', false],
      ['testo', true, 'abc', '4,5', true], ['vuoti', true, '', '', false], ['spenta', false, '10', '12', false]]) {
      fa(nome);
      o[`${nome}.messaggio`] = await salva(acceso, min, max, comeTesto);
      o[`${nome}.errore`] = await errore(page);
      await pagina();
      o[`${nome}.campi`] = await leggiStima();
    }
    fa('accesa');
    o['accesa.messaggio'] = await salva(true, '10', '12', false);
    await pagina();
    o['accesa.campi'] = await leggiStima();
    o['accesa.oggi'] = await oggi();
    await scheda(10, 'accesa.scheda10');
    await scheda(5, 'accesa.scheda5');
  } finally {
    if (!ripristinato) await ripristina().catch(e => { erroreRipristino = String(e && e.message || e).split('\n')[0].replace(/https:\/\/[^\s/]+/g, '<app>'); });
  }
  fa('ripristino');
  await pagina();
  o['ripristino.errore'] = erroreRipristino;
  o['ripristino.campi'] = await leggiStima();
  o['ripristino.uguale'] = String(o['ripristino.campi'] === o['originale.campi']);
  await scheda(10, 'ripristino.scheda10');
  fa('registro');
  o['registro.settings-estimate-updated'] = String((await registro()) - prima);
}

// sessioni-consegna (slice 077): la lista delle consegne e la sessione, nelle due app. Sul database vuoto solo la
// lista («Nessun artigiano in anagrafica.»). Con i dati, per vista, una consegna per l'artigiano 1 («Laboratorio
// Sintetico», con la consegna 1 della cattura ancora aperta): «Apri sessione» senza scelta, poi con l'artigiano;
// «✓ Conferma senza stampare» a sessione vuota (rifiutata); l'aggiunta per etichetta (D-64: il Go vuole il numero
// dell'etichetta, lo standalone il numero interno; la 27/26, 0048/26, sul PC e la 25/26, 0045/25, sul telefono) con
// Invio, come il lettore a pistola, il cursore che resta nel campo, la stessa di nuovo («già in lista»), la lista
// «Sessioni aperte oggi», l'aggiunta per QR (la 30/26, RIP-2026-0051), un codice sconosciuto, ✕ sulla 30/26 (che
// torna libera per la vista dopo) e la conferma con la busta dell'etichetta, che resta fuori bottega. Nei messaggi e
// nelle righe il numero della busta si scrive «#N»: il numero visibile del Go (D-64) lo guarda solo
// etichetta.numero (solo Go). Le righe handover.confirmed si contano prima e dopo.
// Ordine: per ogni app si fanno prima tutti gli scenari sul PC e poi tutti sul telefono, ognuno nell'ordine dei
// nomi. Sul PC lo scenario gira dopo pannello-qr (e dopo schede), che vuole la home senza «📤 Fuori» dopo il rientro
// della 4/26: la busta confermata qui resta fuori bottega. Dopo, sul PC, girano sessioni-rientro (slice 080: la
// 6/26 in una consegna sua di «Incastonatore Sintetico», rientrata), sfondo, tentativi, testi-invii, visualizzatore e
// whatsapp; poi sul telefono tutte le viste (anagrafica-artigiani … pannello-qr, riquadro-azioni,
// salva-avverti, schede, questo, sessioni-rientro, sfondo … whatsapp) con la 27/26 già fuori bottega: nessuna guarda «📤 Fuori» sul
// telefono né la 25/26, la 27/26 o la 30/26 (tranne sessioni-rientro, che gira dopo questo: legge il messaggio della
// conferma su «/?fuori_bottega=1» e lavora sulla sua 6/26) (whatsapp sul telefono usa la 39/26; la 25/26 la usa whatsapp sul PC, che
// gira prima della vista del telefono di questo scenario). lettore-qr gira per ultimo e non le guarda.
async function sessioniConsegna(page, {base, app, vista, dati}, o) {
  page.__app = app;
  const lista = async () => {
    await page.goto(`${base}/handovers`);
    await pronta(page, app);
  };
  const conN = s => s.replace(/#[0-9]+\/[0-9]{2}/g, '#N');
  const conta = async () => testo(await page.locator('#handover-bag-count').innerText());
  const attendi = async t => {
    await page.waitForFunction(x => !!document.body && document.body.innerText.includes(x), t);
    await pronta(page, app);
  };
  const scrivi = async codice => {
    await page.locator('#handover-bag-number').fill(codice);
    await page.locator('#handover-bag-number').press('Enter');
  };
  fa('pagina');
  await lista();
  o['pagina.titolo'] = testo(await page.locator('h1').first().innerText());
  o['pagina.nuova'] = testo(await page.locator('#handover-new h2').innerText());
  o['pagina.menu'] = (await page.locator('#handover-artisan-id option').allInnerTexts()).map(testo).join(' | ');
  o['pagina.pulsante'] = testo(await page.locator('#new-handover-form button[type="submit"]').innerText());
  o['pagina.oggi'] = String(await page.locator('#handovers-open-today').count());
  o['pagina.nessuno'] = (await page.locator('#handover-no-artisans').count()) ? testo(await page.locator('#handover-no-artisans').innerText()) : NESSUNO;
  if (!dati) return;
  const prima = await contaRegistro(page, base, 'handover.confirmed');
  let finestra = NESSUNO;
  page.on('dialog', d => { finestra = `${d.type()}: ${d.message()}`; return d.accept(); });
  const pc = vista === 'desktop';
  // Sul telefono un'etichetta del 2025: con la regola del numero scritto (review della 077) «0047/26» valeva anche
  // per la busta senza etichetta con il numero interno 47/26, creata dagli scenari prima (banco s077p: «Più buste
  // corrispondono…» nel Go). Nel 2025 i soli numeri interni sono 9999/25 e 10000/25: «0045/25» è una busta sola.
  const etichetta = pc ? {go: '0048/26', standalone: '27/26'} : {go: '0045/25', standalone: '25/26'};
  fa('senza-scelta');
  await lista();
  await page.locator('#new-handover-form button[type="submit"]').click();
  await attendi('Seleziona un artigiano.');
  o['senza-scelta.errore'] = await errore(page);
  fa('apri');
  await page.locator('#handover-artisan-id').selectOption('1');
  await page.locator('#new-handover-form button[type="submit"]').click();
  await page.waitForURL(/\/handovers\/[0-9]+$/);
  await pronta(page, app);
  const sessione = percorso(page);
  o['apri.titolo'] = testo(await page.locator('h1').first().innerText());
  o['apri.sessione'] = testo(await page.locator('h1 + p').first().innerText()).replace(/#[0-9]+/, '#S').replace(/[0-9]{2}\/[0-9]{2}\/[0-9]{4} [0-9]{2}:[0-9]{2}/, '<data>');
  o['apri.indietro'] = await page.locator('#handover-back').getAttribute('href');
  o['apri.vuota'] = testo(await page.locator('#handover-empty').innerText());
  o['apri.conteggio'] = await conta();
  o['apri.etichetta-campo'] = testo(await page.locator('label[for="handover-bag-number"]').innerText());
  fa('conferma-vuota');
  finestra = NESSUNO;
  await page.locator('#handover-confirm').click();
  await attendi('Aggiungi almeno una busta.');
  o['conferma-vuota.finestra'] = finestra;
  o['conferma-vuota.errore'] = await errore(page);
  fa('etichetta');
  await scrivi(etichetta[app]);
  await page.waitForFunction(() => (document.getElementById('handover-bag-count') || {}).textContent === '1');
  await attendi('✓ Aggiunta busta');
  const aggiunta = await messaggio(page);
  o['etichetta.messaggio'] = conN(aggiunta);
  o['etichetta.numero'] = (aggiunta.match(/#[0-9]+\/[0-9]{2}/) || [NESSUNO])[0];
  o['etichetta.conteggio'] = await conta();
  o['etichetta.campo'] = JSON.stringify(await page.locator('#handover-bag-number').inputValue());
  o['etichetta.fuoco'] = (await page.evaluate(() => (document.activeElement && document.activeElement.id) || '')) || NESSUNO;
  fa('ripetuta');
  await scrivi(etichetta[app]);
  await attendi('già in lista');
  o['ripetuta.messaggio'] = conN(await messaggio(page));
  o['ripetuta.conteggio'] = await conta();
  fa('oggi');
  await lista();
  o['oggi.righe'] = (await page.locator('#handovers-open-today li').allInnerTexts()).map(t => testo(t).replace(/\s*[0-9]{2}:[0-9]{2}$/, '')).join(' | ') || NESSUNO;
  await page.locator(`#handovers-open-today a[href="${sessione}"]`).click();
  await page.waitForURL(u => new URL(u).pathname === sessione);
  await pronta(page, app);
  o['oggi.ritorno'] = await conta();
  fa('qr');
  await scrivi('RIP-2026-0051');
  await page.waitForFunction(() => (document.getElementById('handover-bag-count') || {}).textContent === '2');
  await pronta(page, app);
  o['qr.messaggio'] = conN(await messaggio(page));
  o['qr.righe'] = (await page.locator('#handover-bags li').allInnerTexts()).map(t => conN(testo(t))).join(' | ');
  fa('sconosciuto');
  await scrivi('SINT-NIENTE-077');
  await attendi('SINT-NIENTE-077»');
  o['sconosciuto.errore'] = await errore(page);
  o['sconosciuto.campo'] = JSON.stringify(await page.locator('#handover-bag-number').inputValue());
  o['sconosciuto.conteggio'] = await conta();
  fa('togli');
  finestra = NESSUNO;
  await page.locator('#handover-remove-30').click();
  await attendi('Busta rimossa dalla consegna.');
  await page.waitForFunction(() => (document.getElementById('handover-bag-count') || {}).textContent === '1');
  o['togli.finestra'] = finestra;
  o['togli.messaggio'] = await messaggio(page);
  o['togli.conteggio'] = await conta();
  o['togli.riga'] = String(await page.locator('#handover-bag-30').count());
  fa('conferma');
  finestra = NESSUNO;
  await page.locator('#handover-confirm').click();
  await page.waitForURL(u => new URL(u).search === '?fuori_bottega=1');
  await pronta(page, app);
  await attendi('Consegna confermata');
  o['conferma.finestra'] = finestra;
  o['conferma.percorso'] = `${percorso(page)}${new URL(page.url()).search}`;
  o['conferma.messaggio'] = await messaggio(page);
  fa('confermata');
  await page.goto(`${base}${sessione}`);
  await pronta(page, app);
  o['confermata.segno'] = (await page.locator('#handover-confirmed-badge').count()) ? testo(await page.locator('#handover-confirmed-badge').innerText()) : NESSUNO;
  o['confermata.campo'] = String(await page.locator('#handover-bag-number').count());
  o['confermata.togli'] = String(await page.locator('[id^="handover-remove-"]').count());
  o['confermata.conferma'] = String(await page.locator('#handover-confirm').count());
  o['confermata.righe'] = (await page.locator('#handover-bags li').allInnerTexts()).map(t => conN(testo(t))).join(' | ');
  fa('registro');
  o['registro.handover-confirmed'] = String((await contaRegistro(page, base, 'handover.confirmed')) - prima);
}

// sessioni-rientro (slice 080): il rientro dall'artigiano dalla scheda, nelle due app (ART-13, RIP-S-16), su **dati
// suoi** (regola degli scenari che cambiano i dati, SVILUPPO.md): ogni vista crea la sua busta da «Nuova riparazione»
// (cliente «Cliente rientro 080 <vista>», QR «SINT-RIENTRO-080-<vista>», accettata il 02/06/2025: il numero è del
// 2025, in fondo alla lista dopo tutte le buste del 2026 che gli altri scenari leggono dalla prima pagina, e non tocca
// la numerazione del 2026 su cui contano sessioni-consegna e lettore-qr; nello standalone la data non basta e il
// numero 8080/25 o 8081/25 si scrive in «N° Busta», numeroDel2025, dalla slice 082; il numero non si confronta,
// perché il contatore del 2025 può essere diverso nelle due app), senza artigiano. Una consegna per «Incastonatore Sintetico» la
// prende per QR e la conferma; sulla scheda il riquadro «🔧 Fuori bottega» (nome, «Consegnata il», l'esito scelto, gli
// esiti, nessun avviso, «Ristampa» e «Condividi»); «✓ Segna rientrata», una nota e «Conferma rientro» lasciando
// l'esito «Fatto» (il predefinito) con la conferma: «Rientro registrato.», stato «Pronta», la data di pronta scritta,
// il riquadro sparito, la nota; la consegna chiusa (nel menu dell'artigiano niente «(sessione aperta)»); le righe
// del registro contate dopo la creazione. Alla fine «Modifica» toglie l'artigiano alla busta («— Nessuno —»): il conto
// delle buste di «Incastonatore Sintetico» che anagrafica-artigiani guarda sul telefono («ha 1 riparazione
// associata», «Elimina» rifiutato sul primo artigiano per nome) torna quello di prima. L'email di pronta non si
// confronta (D-16: il Go del banco non ha l'email accesa, come la 070b).
// Perché «Incastonatore Sintetico» e non l'artigiano 1: la consegna confermata di sessioni-consegna resta aperta e il
// menu dell'artigiano 1 direbbe sempre «(sessione aperta)», senza far vedere la chiusura. Nessun artigiano nuovo:
// uno con una consegna non si può più eliminare, e il menu della lista e l'anagrafica del telefono lo vedrebbero.
// Ordine: gira dopo sessioni-consegna. Se lo scenario si ferma a metà sul PC restano solo dati suoi (la sua busta
// fuori, una consegna aperta di «Incastonatore Sintetico»): sul telefono sessioni-consegna lo vede in pagina.menu e
// anagrafica-artigiani nel conto delle buste (inattese, non timeout).
async function sessioniRientro(page, {base, app, vista, dati}, o) {
  page.__app = app;
  if (!dati) {
    // Sul database vuoto non c'è nessun artigiano: solo la pagina delle consegne.
    await page.goto(`${base}/handovers`);
    await pronta(page, app);
    o['vuoto.nessuno'] = (await page.locator('#handover-no-artisans').count()) ? testo(await page.locator('#handover-no-artisans').innerText()) : NESSUNO;
    return;
  }
  const artigiano = 'Incastonatore Sintetico';
  const codice = `SINT-RIENTRO-080-${vista}`;
  let finestra = NESSUNO;
  page.on('dialog', d => { finestra = `${d.type()}: ${d.message()}`; return d.accept(); });
  const menu = async () => {
    await page.goto(`${base}/handovers`);
    await pronta(page, app);
    return (await page.locator('#handover-artisan-id option').allInnerTexts()).map(testo).filter(t => t.startsWith(artigiano)).join(' | ') || NESSUNO;
  };
  const prontaDal = async () => (await page.locator('#repair-ready-on').count()) ? testo(await page.locator('#repair-ready-on').innerText()).replace(DATA_ORA, '<data>') : NESSUNO;
  fa('prepara');
  await page.goto(`${base}/nuova`);
  await pronta(page, app);
  await page.locator('[name="repair[customer_name]"]').fill(`Cliente rientro 080 ${vista}`);
  await page.locator('[name="repair[description]"]').fill(`Anello del rientro 080 ${vista}`);
  await page.locator('[name="repair[accepted_on]"]').fill('2025-06-02');
  await page.locator('[name="repair[qr_code]"]').fill(codice);
  await numeroDel2025(page, vista === 'desktop' ? '8080/25' : '8081/25');
  await page.locator('#new-repair-form button[type=submit]').click();
  await page.locator(SEL_INFO, {hasText: SALVATA}).first().waitFor();
  await pronta(page, app);
  o['prepara.messaggio'] = SALVATA.test(await messaggio(page)) ? 'salvata' : await messaggio(page);
  const azioni = ['repair.returned_from_artisan', 'repair.returned_from_artisan_auto', 'handover.confirmed', 'repair.status_changed'];
  const prima = {};
  for (const a of azioni) prima[a] = await contaRegistro(page, base, a);
  fa('consegna');
  o['consegna.menu'] = await menu();
  await page.locator('#handover-artisan-id').selectOption({label: artigiano});
  await page.locator('#new-handover-form button[type="submit"]').click();
  await page.waitForURL(/\/handovers\/[0-9]+$/);
  await pronta(page, app);
  await page.locator('#handover-bag-number').fill(codice);
  await page.locator('#handover-bag-number').press('Enter');
  await page.waitForFunction(() => (document.getElementById('handover-bag-count') || {}).textContent === '1');
  await pronta(page, app);
  const busta = Number(((await page.locator('#handover-bags li[id^="handover-bag-"]').first().getAttribute('id')) || '').replace('handover-bag-', ''));
  await page.locator('#handover-confirm').click();
  await page.waitForURL(u => new URL(u).search === '?fuori_bottega=1');
  await pronta(page, app);
  o['consegna.messaggio'] = await messaggio(page);
  fa('scheda');
  await apriScheda(page, base, app, busta);
  await page.locator('#repair-handed-over').waitFor();
  o['scheda.titolo'] = testo(await page.locator('#repair-handed-over strong').first().innerText());
  o['scheda.dal'] = testo(await page.locator('#repair-handed-over-since').innerText()).replace(DATA_ORA, '<data>');
  o['scheda.pulsante'] = testo(await page.locator('#return-from-artisan').innerText());
  o['scheda.esito'] = await page.locator('#return_stato_lavoro').inputValue();
  o['scheda.esiti'] = (await page.locator('#return_stato_lavoro option').allTextContents()).map(testo).join(' | ');
  o['scheda.avviso'] = String(await page.locator('#repair-return-outcome-warning').count());
  o['scheda.ristampa'] = `${testo(await page.locator('#repair-handed-over-reprint').innerText())} ${(await page.locator('#repair-handed-over-reprint').getAttribute('href')).replace(/[0-9]+/, 'ID')} ${await page.locator('#repair-handed-over-reprint').getAttribute('target')}`;
  o['scheda.condividi'] = `${testo(await page.locator('#repair-handed-over-share').innerText())} ${(await page.locator('#repair-handed-over-share').getAttribute('href')).replace(/[0-9]+/, 'ID')} ${(await page.locator('#repair-handed-over-share').getAttribute('data-share-pdf')).replace(/[0-9]+/, 'ID')}`;
  o['scheda.note'] = String(await page.locator('#repair-artisan-notes').count());
  const dataPronta = await prontaDal();
  fa('rientro');
  finestra = NESSUNO;
  await page.locator('#return-from-artisan').click();
  await page.locator('#return_note_artigiano').fill(`  Nota del banco 080 ${vista}  `);
  o['rientro.modulo'] = testo(await page.locator('#repair-return-form h3').innerText());
  await page.locator('#repair-return-form button[type="submit"]').click();
  // Un rifiuto non porta a «Pronta»: si aspetta lo stato o il messaggio d'errore, così l'esito diverso si vede nelle
  // osservazioni e non in un timeout.
  await page.locator(`#repair-status:has-text("Pronta"), ${SEL_ERRORE}`).first().waitFor();
  await page.locator(SEL_INFO).first().waitFor({timeout: 5000}).catch(() => {});
  await pronta(page, app);
  o['rientro.finestra'] = finestra;
  o['rientro.messaggio'] = await messaggio(page);
  o['rientro.errore'] = await errore(page);
  o['rientro.stato'] = testo(await page.locator('#repair-status').innerText());
  o['rientro.pronta'] = `${dataPronta} → ${await prontaDal()}`;
  o['rientro.riquadro'] = String(await page.locator('#repair-handed-over').count());
  o['rientro.note'] = (await page.locator('#repair-artisan-notes').count()) ? testo(await page.locator('#repair-artisan-notes').innerText()) : NESSUNO;
  fa('sessione');
  o['sessione.menu'] = await menu();
  fa('registro');
  for (const a of azioni) o[`registro.${a.replace(/[._]/g, '-')}`] = String((await contaRegistro(page, base, a)) - prima[a]);
  fa('fine');
  await apriScheda(page, base, app, busta);
  await page.locator('#edit-repair').click();
  await page.locator('#repair_artisan_id').waitFor({state: 'visible'});
  await page.locator('#repair_artisan_id').selectOption('');
  await page.locator('#save-repair').click();
  await page.locator('#repair-artisan', {hasText: '—'}).waitFor();
  await pronta(page, app);
  o['fine.artigiano'] = testo(await page.locator('#repair-artisan').innerText());
}

// storico-correzioni (slice 082): «✕ Sgancia» e «✕ Elimina dallo storico» del titolare nel dettaglio dello storico
// delle consegne, nelle due app (ART-16, 17, 18), con le loro domande, su **dati suoi** (regola degli scenari che
// cambiano i dati, SVILUPPO.md): ogni vista crea due buste da «Nuova riparazione» (clienti «Cliente storico 082 <vista>
// A|B», QR «SINT-STORICO-082-<vista>-A|B», accettate il 03/06/2025 con, nello standalone, il numero 8082/25 e 8083/25
// sul PC, 8084/25 e 8085/25 sul telefono scritto in «N° Busta» (numeroDel2025): numeri del 2025 nelle due app, in
// fondo alla lista, che non si confrontano), apre una consegna di «Incastonatore Sintetico», le prende per QR e la conferma; le fa rientrare dalla
// scheda con «Nessun cambio di stato» (la A con una nota), così la consegna si chiude e lo stato delle buste non
// cambia. Nel dettaglio: titolo, le due righe, i pulsanti; «✕ Sgancia» sulla B con la domanda («Busta sganciata dalla
// consegna.», resta una busta), poi «✕ Elimina dallo storico» con la domanda («Consegna #N eliminata dallo storico (1
// buste sganciate).», la riga sparita dall'elenco). Le righe di sgancio ed eliminazione contate dopo la creazione. Alla fine
// «Modifica» toglie l'artigiano alle due buste («— Nessuno —»), come sessioni-rientro: il conto delle buste di
// «Incastonatore Sintetico» che anagrafica-artigiani guarda sul telefono resta quello di prima, e la consegna non c'è
// più (nessuna «(sessione aperta)»). Numeri di busta e di consegna e istanti si scrivono <numero>, N e <data> (D-64:
// «#12/25» nello standalone, «N. 12/25» nel Go per un QR che non è un'etichetta). Ordine: sul PC gira dopo
// sessioni-rientro e sfondo, prima di tentativi; nessuno scenario dopo guarda le sue buste. Se si ferma a metà
// restano solo dati suoi (le due buste, una consegna di «Incastonatore Sintetico» aperta o chiusa): sul telefono
// sessioni-consegna e sessioni-rientro lo vedono nel menu dell'artigiano e anagrafica-artigiani nel conto delle buste.
async function storicoCorrezioni(page, {base, app, vista, dati}, o) {
  page.__app = app;
  if (!dati) {
    await page.goto(`${base}/handovers/history`);
    await pronta(page, app);
    o['vuoto.elenco'] = (await page.locator('#handover-history-empty').count()) ? testo(await page.locator('#handover-history-empty').innerText()) : NESSUNO;
    return;
  }
  const artigiano = 'Incastonatore Sintetico';
  let finestra = NESSUNO;
  page.on('dialog', d => { finestra = `${d.type()}: ${d.message()}`; return d.accept(); });
  const NUMERO = /(#|N\. )[0-9]+\/25/g;
  const pulito = t => testo(t).replace(NUMERO, '<numero>').replace(DATA_ORA, '<data>');
  fa('prepara');
  const lettere = ['A', 'B'];
  for (const l of lettere) {
    await page.goto(`${base}/nuova`);
    await pronta(page, app);
    await page.locator('[name="repair[customer_name]"]').fill(`Cliente storico 082 ${vista} ${l}`);
    await page.locator('[name="repair[description]"]').fill(`Anello dello storico 082 ${vista} ${l}`);
    await page.locator('[name="repair[accepted_on]"]').fill('2025-06-03');
    await page.locator('[name="repair[qr_code]"]').fill(`SINT-STORICO-082-${vista}-${l}`);
    await numeroDel2025(page, `${(vista === 'desktop' ? 8082 : 8084) + lettere.indexOf(l)}/25`);
    await page.locator('#new-repair-form button[type=submit]').click();
    await page.locator(SEL_INFO, {hasText: SALVATA}).first().waitFor();
    await pronta(page, app);
  }
  o['prepara.messaggio'] = SALVATA.test(await messaggio(page)) ? 'salvata' : await messaggio(page);
  // Le letture dello storico (handover.history_viewed, …_detail_viewed) non si contano qui: una LiveView le scrive al
  // primo disegno e di nuovo al collegamento, il Go una volta; le confronta lo scenario HTTP storico-consegne.
  const azioni = ['handover.bag_detached', 'handover.deleted'];
  const prima = {};
  for (const a of azioni) prima[a] = await contaRegistro(page, base, a);
  fa('consegna');
  await page.goto(`${base}/handovers`);
  await pronta(page, app);
  await page.locator('#handover-artisan-id').selectOption({label: artigiano});
  await page.locator('#new-handover-form button[type="submit"]').click();
  await page.waitForURL(/\/handovers\/[0-9]+$/);
  await pronta(page, app);
  const consegna = Number(new URL(page.url()).pathname.split('/').pop());
  for (const [i, l] of lettere.entries()) {
    await page.locator('#handover-bag-number').fill(`SINT-STORICO-082-${vista}-${l}`);
    await page.locator('#handover-bag-number').press('Enter');
    await page.waitForFunction(n => (document.getElementById('handover-bag-count') || {}).textContent === n, String(i + 1));
    await pronta(page, app);
  }
  // La lista della consegna è dalla più recente: la B prima.
  const buste = (await page.locator('#handover-bags li[id^="handover-bag-"]').evaluateAll(ll => ll.map(l => Number(l.id.replace('handover-bag-', ''))))).reverse();
  await page.locator('#handover-confirm').click();
  await page.waitForURL(u => new URL(u).search === '?fuori_bottega=1');
  await pronta(page, app);
  o['consegna.messaggio'] = await messaggio(page);
  fa('rientro');
  for (const [i, busta] of buste.entries()) {
    await apriScheda(page, base, app, busta);
    await page.locator('#return-from-artisan').click();
    await page.locator('#return_stato_lavoro').selectOption('');
    if (i === 0) await page.locator('#return_note_artigiano').fill(`Nota dello storico 082 ${vista}`);
    await page.locator('#repair-return-form button[type="submit"]').click();
    await page.locator(`${SEL_INFO}, ${SEL_ERRORE}`).first().waitFor();
    await pronta(page, app);
    o[`rientro.messaggio-${lettere[i].toLowerCase()}`] = await messaggio(page);
  }
  fa('dettaglio');
  await page.goto(`${base}/handovers/history/${consegna}`);
  await pronta(page, app);
  o['dettaglio.titolo'] = testo(await page.locator('h1').first().innerText()).replace(`#${consegna}`, '#N');
  o['dettaglio.righe'] = (await page.locator('#handover-history-bags li').allInnerTexts()).map(pulito).join(' | ');
  o['dettaglio.sgancia'] = (await page.locator('[id^="handover-history-detach-"] button').allInnerTexts()).map(testo).join(' | ') || NESSUNO;
  o['dettaglio.elimina'] = (await page.locator(`#handover-history-delete-${consegna} button`).count()) ? testo(await page.locator(`#handover-history-delete-${consegna} button`).innerText()) : NESSUNO;
  fa('sgancia');
  finestra = NESSUNO;
  await page.locator(`#handover-history-detach-${buste[1]} button`).click();
  await page.locator(`${SEL_INFO}, ${SEL_ERRORE}`).first().waitFor();
  await pronta(page, app);
  o['sgancia.finestra'] = finestra.replace(NUMERO, '<numero>');
  o['sgancia.messaggio'] = await messaggio(page);
  o['sgancia.errore'] = await errore(page);
  o['sgancia.indirizzo'] = new URL(page.url()).pathname.replace(String(consegna), 'N');
  o['sgancia.buste'] = String(await page.locator('#handover-history-bags li').count());
  fa('elimina');
  finestra = NESSUNO;
  await page.locator(`#handover-history-delete-${consegna} button`).click();
  await page.waitForURL(u => new URL(u).pathname === '/handovers/history');
  await pronta(page, app);
  o['elimina.finestra'] = finestra;
  o['elimina.messaggio'] = (await messaggio(page)).replace(`#${consegna}`, '#N');
  o['elimina.errore'] = await errore(page);
  o['elimina.riga'] = String(await page.locator(`#handover-history-${consegna}`).count());
  fa('registro');
  for (const a of azioni) o[`registro.${a.replace(/[._]/g, '-')}`] = String((await contaRegistro(page, base, a)) - prima[a]);
  fa('fine');
  for (const busta of buste) {
    await apriScheda(page, base, app, busta);
    await page.locator('#edit-repair').click();
    await page.locator('#repair_artisan_id').waitFor({state: 'visible'});
    await page.locator('#repair_artisan_id').selectOption('');
    await page.locator('#save-repair').click();
    await page.locator('#repair-artisan', {hasText: '—'}).waitFor();
    await pronta(page, app);
  }
  o['fine.artigiano'] = testo(await page.locator('#repair-artisan').innerText());
}

// salva-avverti (slice 068): «📲 Salva e avverti cliente» del modulo di modifica, nelle due app, sulla
// 40/26 (10000/25, senza QR, aperta, di nessun altro scenario): la busta va pronta dal menu dello stato,
// il modulo ha il pulsante; un salvataggio rifiutato (descrizione vuota, con il controllo del browser
// spento) resta sul modulo senza popup; un salvataggio con il prezzo apre il popup dell'avviso di ritiro
// (con JavaScript nel Go il pulsante spento al primo invio manda lo stesso then=notify). Sul PC «Annulla»
// torna alla scheda con il prezzo salvato e niente registrato; sul telefono «Invia» con un testo senza
// link (D-74: WhatsApp e la lista), poi «Azzera l'avviso». Alla fine la busta torna in lavorazione.
async function salvaAvverti(page, {base, app, vista, dati}, o) {
  const busta = 40;
  if (!dati) return bustaAssente(page, base, app, busta, o, `busta-${busta}`);
  page.__app = app;
  const pc = vista === 'desktop';
  const azioni = ['repair.updated', 'outgoing.handed_off', 'ready_notice.sent', 'repair.public_link_issued'];
  const prima = {};
  for (const a of azioni) prima[a] = await contaRegistro(page, base, a);
  fa('pronta');
  await apriScheda(page, base, app, busta);
  await page.locator('#transition_status').selectOption('pronta');
  await page.locator('#repair-status-form button').click();
  await page.locator('#repair-notice').waitFor();
  await pronta(page, app);
  fa('modulo');
  await page.locator('#edit-repair').click();
  await page.locator('#repair-form').waitFor();
  await pronta(page, app);
  o['modulo.pulsanti'] = (await page.locator('#repair-form button[type=submit]').allInnerTexts()).map(testo).join(' | ');
  fa('rifiuto');
  await page.evaluate(() => { document.getElementById('repair-form').noValidate = true; });
  await page.locator('#repair_description').fill('');
  await page.locator('#save-and-notify').click();
  await page.locator('#repair-form').waitFor();
  await attendi(1000);
  await pronta(page, app);
  o['rifiuto.popup'] = String(await page.locator('#notice-popup').count());
  o['rifiuto.modulo'] = String(await page.locator('#repair-form').count());
  o['rifiuto.errore-campo'] = await leggi(page, '#repair-form p.field-error, #repair-form p.text-error');
  fa('salva');
  await apriScheda(page, base, app, busta);
  await page.locator('#edit-repair').click();
  await page.locator('#repair-form').waitFor();
  await pronta(page, app);
  const prezzo = pc ? '45,50' : '30';
  await page.locator('#repair_final_price_euros').fill(prezzo);
  await page.locator('#save-and-notify').click();
  const via = await leggiPopup(page, o, 'popup');
  o['popup.messaggio'] = await messaggio(page);
  if (pc) {
    fa('annulla');
    await page.locator('#notice-cancel').click();
    await page.locator('#notice-popup').waitFor({state: 'detached'});
    await pronta(page, app);
    o['annulla.percorso'] = percorso(page);
    o['annulla.modulo'] = String(await page.locator('#repair-form').count());
  } else {
    fa('invio');
    await page.locator('#notice-body').fill('Messaggio del banco 068, senza link');
    await inviaPopup(page, o, 'invio', via);
  }
  fa('dopo');
  await apriScheda(page, base, app, busta);
  o['dopo.prezzo'] = testo(await page.locator('#repair-final-price').innerText());
  o['dopo.avviso'] = testo(await page.locator('#repair-notice').innerText()).replace(DATA_ORA, '<data>');
  if (!pc) {
    fa('azzera');
    page.once('dialog', d => d.accept());
    await page.locator('#repair-notice-reset').click();
    await page.locator('#repair-notice-pending').waitFor();
    await pronta(page, app);
  }
  fa('ritorno');
  await apriScheda(page, base, app, busta);
  await page.locator('#transition_status').selectOption('in_lavorazione');
  await page.locator('#repair-status-form button').click();
  await page.locator('#repair-status', {hasText: 'In lavorazione'}).waitFor();
  fa('registro');
  for (const a of azioni) o[`registro.${a.replace(/[._]/g, '-')}`] = String((await contaRegistro(page, base, a)) - prima[a]);
}

// provider-posta (slice 069b): la pagina «Provider di invio» del titolare, parte email, nelle due app, senza
// nessun server di posta (solo .invalid, mai una connessione: nessuna prova parte con una configurazione).
// Desktop: due salvataggi rifiutati (gli errori del changeset sotto i campi, i valori mandati, la casella
// della password vuota, «Non configurato»), uno valido («Provider salvato.», «Configurato», la password
// mai rimostrata ma il segnaposto «salvata — …»), «Cancella le credenziali salvate», poi la configurazione
// rimessa vuota com'era e «✉ Invia messaggio di prova» senza configurazione (niente accodato). Le righe
// outgoing.provider_configured e outgoing.queued si contano prima e dopo. Telefono: la pagina e basta.
// L'interruttore dell'email e le email rimaste in coda (D-03, D-27) sono solo del Go: li provano i test Go.
// PAROLA_BANCO è la password finta del banco: non va mai su una pagina.
const PAROLA_BANCO = ['PAROLA', 'FINTA', 'banco', '069b'].join('-');

async function providerPosta(page, {base, app, vista}, o) {
  page.__app = app;
  const azioni = ['outgoing.provider_configured', 'outgoing.queued'];
  const prima = {};
  if (vista === 'desktop') for (const a of azioni) prima[a] = await contaRegistro(page, base, a);
  const apri = async () => { await page.goto(`${base}/settings/providers`); await pronta(page, app); };
  const stato = async () => testo(await page.locator('section:has(#smtp-form) h2 + span').first().innerText());
  const errori = async id => (await page.locator(`#${id}`).locator('xpath=ancestor::div[1]').locator('p.text-error, p.field-error')
    .allInnerTexts()).map(testo).join(' | ') || NESSUNO;
  const valori = async () => {
    const v = {};
    for (const c of ['host', 'port', 'security', 'username', 'password', 'from_address', 'from_name']) v[c] = await page.locator(`#smtp_${c}`).inputValue();
    return JSON.stringify(v);
  };
  const compila = async v => {
    for (const [c, x] of Object.entries(v)) {
      if (c === 'security') await page.locator('#smtp_security').selectOption(x);
      else await page.locator(`#smtp_${c}`).fill(x);
    }
  };
  // Il modulo si manda con il pulsante; nello standalone LiveView ridisegna sul posto, nel Go torna la
  // pagina. Si aspettano errori dei campi diversi da quelli di prima (slice 071: nello standalone gli errori
  // del primo rifiuto restano sulla pagina finché LiveView non ridisegna, e il secondo rifiuto li leggeva
  // vecchi) o il messaggio.
  const salva = async attesa => {
    const SEL = '#smtp-form p.text-error, #smtp-form p.field-error';
    const prima = (await page.locator(SEL).allInnerTexts()).join('|');
    await page.evaluate(() => { document.getElementById('smtp-form').noValidate = true; });
    await page.locator('#smtp-form button').click();
    if (attesa === 'errore') {
      // Con i localizzatori (il Go cambia pagina: una funzione nella pagina si perderebbe), al più 30 s.
      const fine = Date.now() + 30000;
      for (;;) {
        const ora = (await page.locator(SEL).allInnerTexts().catch(() => [])).join('|');
        if (ora !== '' && ora !== prima) break;
        if (Date.now() > fine) throw new Error('errori del modulo della posta non arrivati');
        await page.waitForTimeout(200);
      }
    }
    else await page.waitForFunction(() => /Provider salvato/.test(document.body ? document.body.innerText : ""));
    await pronta(page, app);
  };
  fa('pagina');
  await apri();
  o['pagina.titolo'] = testo(await page.locator('h1').first().innerText());
  o['pagina.stato'] = await stato();
  o['pagina.valori'] = await valori();
  o['pagina.prova'] = testo(await page.locator('#smtp-test').innerText());
  if (vista !== 'desktop') return;
  fa('rifiuto-1');
  await compila({host: 'smtp.example.invalid', port: '70000', security: 'starttls', username: 'banco-069b', password: '',
    from_address: 'senza-chiocciola', from_name: 'Banco 069b'});
  await salva('errore');
  o['rifiuto-1.stato'] = await stato();
  for (const c of ['host', 'port', 'username', 'password', 'from_address', 'from_name']) o[`rifiuto-1.errori.${c.replace('_', '-')}`] = await errori(`smtp_${c}`);
  o['rifiuto-1.valori'] = await valori();
  o['rifiuto-1.messaggio'] = await messaggio(page);
  fa('rifiuto-2');
  await compila({host: 'h'.repeat(256), port: '0', security: 'none', username: 'banco-069b', password: PAROLA_BANCO,
    from_address: 'banco@example.invalid', from_name: 'è'.repeat(201)});
  await salva('errore');
  for (const c of ['host', 'port', 'password', 'from_address', 'from_name']) o[`rifiuto-2.errori.${c.replace('_', '-')}`] = await errori(`smtp_${c}`);
  o['rifiuto-2.password'] = JSON.stringify(await page.locator('#smtp_password').inputValue());
  o['rifiuto-2.password-in-pagina'] = String((await page.content()).includes(PAROLA_BANCO));
  fa('salva');
  await apri();
  await compila({host: 'smtp.example.invalid', port: '587', security: 'starttls', username: 'banco-069b', password: PAROLA_BANCO,
    from_address: 'banco@example.invalid', from_name: 'Banco 069b'});
  await salva('salvato');
  o['salva.messaggio'] = await messaggio(page);
  o['salva.stato'] = await stato();
  o['salva.valori'] = await valori();
  o['salva.segnaposto'] = await page.locator('#smtp_password').getAttribute('placeholder');
  o['salva.password-in-pagina'] = String((await page.content()).includes(PAROLA_BANCO));
  fa('cancella');
  await apri();
  await page.locator('input[name="smtp[clear_secret]"]').check();
  await salva('salvato');
  await apri();
  o['cancella.valori'] = await valori();
  o['cancella.segnaposto'] = JSON.stringify(await page.locator('#smtp_password').getAttribute('placeholder'));
  fa('ripristina');
  await compila({host: '', port: '', security: 'starttls', username: '', password: '', from_address: '', from_name: ''});
  await salva('salvato');
  await apri();
  o['ripristina.stato'] = await stato();
  o['ripristina.valori'] = await valori();
  fa('prova');
  await page.locator('#smtp-test').click();
  await page.waitForFunction(() => /niente è stato accodato/.test(document.body ? document.body.innerText : ""));
  await pronta(page, app);
  o['prova.errore'] = await errore(page);
  o['prova.coda'] = testo(await page.locator('text=/In coda \\d+ · accettati/').first().innerText());
  fa('registro');
  for (const a of azioni) o[`registro.${a.replace(/[._]/g, '-')}`] = String((await contaRegistro(page, base, a)) - prima[a]);
}

// provider-telegram (slice 071, ADM-10): il riquadro Telegram della pagina dei provider. Mai una
// richiesta al Bot API: il token salvato è finto e di forma valida, le «Notifiche Telegram ai clienti» si
// accendono e si spengono solo senza bot configurato (il lettore dello standalone legge solo con il bot
// configurato e le notifiche accese) e sono spente mentre il token finto è salvato, il bot salvato si
// cancella subito, e la prova si chiede solo senza configurazione. Alla fine (anche dopo un errore) token,
// chat e notifiche tornano come erano: l'import porta le notifiche così come sono nello standalone, quindi
// lo scenario non conta su uno stato di partenza.
const TOKEN_BANCO = ['123456', 'BANCO-071-finto'].join(':');

async function providerTelegram(page, {base, app, vista}, o) {
  page.__app = app;
  const azioni = ['outgoing.provider_configured', 'outgoing.telegram_customer_notifications', 'outgoing.queued'];
  const prima = {};
  if (vista === 'desktop') for (const a of azioni) prima[a] = await contaRegistro(page, base, a);
  const apri = async () => { await page.goto(`${base}/settings/providers`); await pronta(page, app); };
  // Lo stato del riquadro: lo span dopo il titolo «Telegram» (nel Go la pagina sta dentro un'altra
  // <section>, quindi niente «section:has(...)»).
  const stato = async () => testo(await page.locator('h2:text-is("Telegram") + span').first().innerText());
  const clienti = async () => testo(await page.locator('#telegram-customer-state').innerText());
  const errori = async id => (await page.locator(`#${id}`).locator('xpath=ancestor::div[1]').locator('p.text-error, p.field-error')
    .allInnerTexts()).map(testo).join(' | ') || NESSUNO;
  const valori = async () => JSON.stringify({bot_token: await page.locator('#telegram_bot_token').inputValue(),
    chat_id: await page.locator('#telegram_chat_id').inputValue()});
  const salva = async attesa => {
    await page.evaluate(() => { document.getElementById('telegram-form').noValidate = true; });
    await page.locator('#telegram-form button').click();
    if (attesa === 'errore') await page.locator('#telegram-form p.text-error, #telegram-form p.field-error').first().waitFor();
    else await page.waitForFunction(() => /Provider salvato/.test(document.body ? document.body.innerText : ""));
    await pronta(page, app);
  };
  // L'interruttore delle notifiche: il pulsante porta lo stato che chiede. Si aspetta che lo stato del
  // riquadro cambi davvero (nello standalone il messaggio di prima resta sulla pagina: aspettare il
  // messaggio leggeva lo stato vecchio e il clic dopo tornava indietro).
  const notifiche = async () => {
    const da = await clienti();
    await page.locator('#telegram-customer-toggle').click();
    await page.waitForFunction(da => (document.getElementById('telegram-customer-state')?.innerText || '').replace(/\s+/g, ' ').trim() !== da, da);
    await pronta(page, app);
  };
  const notificheA = async voluto => {
    if (await clienti() !== voluto) await notifiche();
    if (await clienti() !== voluto) throw new Error(`notifiche non ${voluto}`);
  };
  fa('pagina');
  await apri();
  o['pagina.stato'] = await stato();
  o['pagina.valori'] = await valori();
  o['pagina.segnaposto'] = JSON.stringify(await page.locator('#telegram_bot_token').getAttribute('placeholder'));
  o['pagina.prova'] = testo(await page.locator('#telegram-test').innerText());
  o['pagina.aiuto-chat'] = testo(await page.locator('#telegram-chat-help').innerText());
  o['pagina.nome-bot'] = testo(await page.locator('#telegram-bot-username').innerText());
  o['pagina.notifiche'] = await clienti();
  o['pagina.pulsante-notifiche'] = testo(await page.locator('#telegram-customer-toggle').innerText());
  if (vista !== 'desktop') return;
  if (o['pagina.stato'] !== 'Non configurato') throw new Error('Telegram configurato nei dati del banco: lo scenario non tocca un bot');
  const notificheIniziali = await clienti();
  const chatIniziale = await page.locator('#telegram_chat_id').inputValue();
  let errore1 = null;
  try {
    fa('notifiche');
    // Spente → accese → spente, qualunque sia lo stato di partenza (senza bot configurato: nessun lettore).
    await notificheA('Spente');
    await notifiche();
    o['notifiche-1.messaggio'] = await messaggio(page);
    o['notifiche-1.stato'] = await clienti();
    o['notifiche-1.senza-bot'] = String((await page.locator('body').innerText()).includes('Il bot non è configurato'));
    await notifiche();
    o['notifiche-2.messaggio'] = await messaggio(page);
    o['notifiche-2.stato'] = await clienti();
    // Prima di salvare un token le notifiche devono essere spente: nessun lettore parte.
    await notificheA('Spente');
    fa('rifiuto');
    await apri();
    await page.locator('#telegram_bot_token').fill('senza-numero');
    await page.locator('#telegram_chat_id').fill('chat sbagliata');
    await salva('errore');
    o['rifiuto.stato'] = await stato();
    o['rifiuto.errori.bot-token'] = await errori('telegram_bot_token');
    o['rifiuto.errori.chat-id'] = await errori('telegram_chat_id');
    o['rifiuto.valori'] = await valori();
    o['rifiuto.messaggio'] = await messaggio(page);
    fa('salva');
    await apri();
    if (await clienti() !== 'Spente') throw new Error('notifiche accese prima del salvataggio');
    await page.locator('#telegram_bot_token').fill(TOKEN_BANCO);
    await page.locator('#telegram_chat_id').fill('-1001');
    await salva('salvato');
    o['salva.messaggio'] = await messaggio(page);
    o['salva.stato'] = await stato();
    o['salva.valori'] = await valori();
    o['salva.segnaposto'] = await page.locator('#telegram_bot_token').getAttribute('placeholder');
    o['salva.token-in-pagina'] = String((await page.content()).includes(TOKEN_BANCO));
    fa('cancella');
    await apri();
    await page.locator('input[name="telegram[clear_secret]"]').check();
    await salva('salvato');
    await apri();
    o['cancella.stato'] = await stato();
    o['cancella.segnaposto'] = JSON.stringify(await page.locator('#telegram_bot_token').getAttribute('placeholder'));
    fa('ripristina');
    await page.locator('#telegram_chat_id').fill(chatIniziale);
    await salva('salvato');
    await apri();
    o['ripristina.stato'] = await stato();
    o['ripristina.valori'] = await valori();
    fa('prova');
    await page.locator('#telegram-test').click();
    await page.waitForFunction(() => /niente è stato accodato/.test(document.body ? document.body.innerText : ""));
    await pronta(page, app);
    o['prova.errore'] = await errore(page);
  } catch (e) {
    errore1 = e;
  }
  // Rimette tutto com'era anche dopo un errore: prima via il token finto (niente bot configurato), poi la
  // chat e le notifiche di partenza.
  try {
    fa('rimetti');
    await apri();
    if (await stato() !== 'Non configurato' || await page.locator('#telegram_chat_id').inputValue() !== chatIniziale) {
      await notificheA('Spente');
      await page.locator('#telegram_bot_token').fill('');
      await page.locator('#telegram_chat_id').fill(chatIniziale);
      await page.locator('input[name="telegram[clear_secret]"]').check();
      await salva('salvato');
      await apri();
    }
    if (await stato() !== 'Non configurato') throw new Error('bot finto ancora salvato');
    await notificheA(notificheIniziali);
  } catch (e) {
    if (!errore1) errore1 = e;
  }
  if (errore1) throw errore1;
  fa('registro');
  for (const a of azioni) o[`registro.${a.replace(/[._]/g, '-')}`] = String((await contaRegistro(page, base, a)) - prima[a]);
}

// consegne-solleciti (slice 083): «⏰ Solleciti pendenti» nelle due app. Una busta propria non può essere in ritardo
// (la consegna la fa uscire adesso e la soglia è di almeno un giorno), quindi lo scenario usa la sola busta in ritardo
// della cattura, la 4/26 della consegna 1 (artigiano 1, «Laboratorio Sintetico»), e non la cambia: scrive solo dei
// solleciti (righe di solleciti_artigiani, che nessun contatore conta: il contatore della lista consegne conta le
// buste in ritardo, e restano 1). Sul PC: la pagina con il gruppo e la busta (numero «N», giorni «<n>gg»), «⏰
// Registra sollecito» con la conferma («Sollecito registrato per 1 busta.», «Ultimo sollecito: <data>»), poi «👥
// Scegli contatto» (registra di nuovo e apre wa.me/?text=… con il testo del Flask, numero della busta «N»; la pagina
// torna alla lista, D-74), il riquadro «⏰ Solleciti all'artigiano» della scheda 4/26 (due righe) e le righe
// repair.artisan_reminded (2). Le letture della pagina non si contano (la LiveView le scrive due volte, come lo
// storico); le confronta lo scenario HTTP solleciti.
// Ordine: il nome lo fa girare sul PC prima di pannello-qr, che fa rientrare la 4/26 dal QR (sul PC): sul telefono la
// 4/26 non è più fuori e la pagina è vuota nelle due app («Nessun sollecito pendente. Tutto in regola. ✓»). Dopo
// questo scenario la 4/26 la guardano solo pannello-qr (modifica, rientro, «📤 Fuori» della home) e nessuno legge i
// suoi solleciti. Sul database vuoto: la pagina vuota.
async function consegneSolleciti(page, {base, app, vista, dati}, o) {
  page.__app = app;
  const vuota = async () => {
    await page.goto(`${base}/handovers/reminders`);
    await pronta(page, app);
    const v = page.locator('#artisan-reminders-empty');
    o['vuoto.elenco'] = (await v.count()) ? testo(await v.innerText()) : NESSUNO;
    o['vuoto.gruppi'] = String(await page.locator('[id^="artisan-reminder-group-"]').count());
  };
  if (!dati || vista !== 'desktop') return vuota();
  const numero = t => t.replace(/(#|N\. )[0-9]+\/[0-9]{2}/g, 'N').replace(/\b[0-9]+gg\b/g, '<n>gg');
  let finestra = NESSUNO;
  page.on('dialog', d => { finestra = `${d.type()}: ${d.message()}`; return d.accept(); });
  const prima = await contaRegistro(page, base, 'repair.artisan_reminded');
  fa('pagina');
  await page.goto(`${base}/handovers/reminders`);
  await pronta(page, app);
  const gruppi = page.locator('[id^="artisan-reminder-group-"]');
  o['pagina.gruppi'] = String(await gruppi.count());
  const id = ((await gruppi.first().getAttribute('id')) || '').replace('artisan-reminder-group-', '');
  o['pagina.artigiano'] = testo(await page.locator(`#artisan-reminder-group-${id} h2`).innerText());
  o['pagina.buste'] = (await page.locator(`#artisan-reminder-group-${id} [id^="artisan-reminder-bag-"]`).allInnerTexts()).map(t => numero(testo(t))).join(' | ') || NESSUNO;
  o['pagina.soglia'] = await page.locator('#artisan-reminders-threshold input[name="giorni"]').inputValue();
  o['pagina.scegli'] = testo(await page.locator(`#reminder-choose-${id}`).innerText());
  o['pagina.registra'] = testo(await page.locator(`#record-reminder-${id}`).innerText());
  o['pagina.ultimo'] = String(await page.locator(`#artisan-reminder-last-${id}`).count());
  fa('registra');
  await page.locator(`#record-reminder-${id}`).click();
  await page.locator(SEL_INFO).first().waitFor();
  o['registra.finestra'] = finestra;
  o['registra.messaggio'] = testo(await page.locator(SEL_INFO).first().innerText());
  await page.locator(`#artisan-reminder-last-${id}`).waitFor();
  o['registra.ultimo'] = testo(await page.locator(`#artisan-reminder-last-${id}`).innerText()).replace(DATA_ORA, '<data>');
  o['registra.indirizzo'] = doveSono(page);
  fa('whatsapp');
  const ctx = page.context();
  const aperte = ctx.waMe.length;
  await Promise.all([page.waitForURL(u => new URL(u).pathname === '/', {timeout: 20000}), page.locator(`#reminder-choose-${id}`).click()]);
  await pronta(page, app);
  for (let i = 0; i < 100 && ctx.waMe.length === aperte; i++) await attendi(100);
  o['wa.dopo'] = doveSono(page);
  o['wa.aperture'] = String(ctx.waMe.length - aperte);
  const u = ctx.waMe.length > aperte ? new URL(ctx.waMe[ctx.waMe.length - 1]) : null;
  o['wa.numero'] = u ? (decodeURIComponent(u.pathname.slice(1)) || '(vuoto)') : NESSUNO;
  o['wa.testo'] = u ? JSON.stringify((u.searchParams.get('text') || '').replace(/[0-9]+\/[0-9]{2}/g, 'N')) : NESSUNO;
  for (const p of ctx.pages()) if (p !== page) await p.close().catch(() => {});
  fa('scheda');
  await apriScheda(page, base, app, 4);
  const riquadro = page.locator('#repair-artisan-reminders');
  o['scheda.righe'] = String(await riquadro.locator('li').count());
  o['scheda.titolo'] = (await riquadro.count()) ? testo(await riquadro.locator('strong').first().innerText()) : NESSUNO;
  o['scheda.prima'] = (await riquadro.count()) ? testo(await riquadro.locator('li').first().innerText()).replace(DATA_ORA, '<data>').replace(/[0-9]+gg/, '<n>gg') : NESSUNO;
  fa('registro');
  o['registro.repair-artisan-reminded'] = String((await contaRegistro(page, base, 'repair.artisan_reminded')) - prima);
}

// sollecito-artigiano (slice 085): «🔧 Sollecita artigiano» della scheda nelle due app (ART-23, CLI-29 parte
// artigiano), su **dati suoi** (regola degli scenari che cambiano i dati, SVILUPPO.md). Sul database vuoto niente. Con
// i dati, per vista: un artigiano nuovo con il telefono («Artigiano Sollecito 085 <vista>») e una busta nuova del
// 2025 («Cliente sollecito 085 <vista>», 8086/25 sul PC e 8087/25 sul telefono, trovata con il suo QR) affidata a
// lui dalla modifica. La scheda: il pulsante; il popup (titolo, piattaforma col numero e il nome, il testo
// wa_artigiano con il numero «N», il suggerimento, i pulsanti); «Invia» con un testo senza {link} (il Go del banco
// non ha indirizzo_pubblico: {link} lo provano i test Go), wa.me al numero dell'artigiano, la lista dopo (D-74); la
// riga «· all'artigiano» in «📨 Invii registrati»; le righe outgoing.handed_off. Alla fine la busta torna senza
// artigiano e l'artigiano si elimina, così gli scenari dopo (menu delle consegne, righe dell'anagrafica sul
// telefono) non lo vedono.
async function sollecitoArtigiano(page, {base, app, vista, dati}, o) {
  if (!dati) return;
  page.__app = app;
  const pc = vista === 'desktop';
  const nome = `Artigiano Sollecito 085 ${vista}`;
  const codice = `SINT-SOLLECITO-085-${vista}`;
  let finestra = NESSUNO;
  page.on('dialog', d => { finestra = `${d.type()}: ${d.message()}`; return d.accept(); });
  const anagrafica = async () => {
    await page.goto(`${base}/settings/anagrafiche`);
    await pronta(page, app);
  };
  const rigaArtigiano = async () => {
    const rr = page.locator('#artisans-list > [id^="artisan-"]');
    for (let i = 0; i < await rr.count(); i++) {
      if (await rr.nth(i).locator('input[name="artisan[name]"]').inputValue() === nome) return rr.nth(i);
    }
    return null;
  };
  const scrittaInPagina = async t => {
    await page.waitForFunction(x => !!document.body && document.body.innerText.includes(x), t);
    await pronta(page, app);
  };
  fa('prepara');
  await anagrafica();
  await page.locator('#new-artisan-name').fill(nome);
  await page.locator('#new-artisan-phone').fill(pc ? '+39 000 085 0001' : '+39 000 085 0002');
  await page.locator('#new-artisan-form button[type="submit"]').click();
  await scrittaInPagina(`Artigiano "${nome}" aggiunto.`);
  await page.goto(`${base}/nuova`);
  await pronta(page, app);
  await page.locator('[name="repair[customer_name]"]').fill(`Cliente sollecito 085 ${vista}`);
  await page.locator('[name="repair[customer_phone]"]').fill('333 000 0850');
  await page.locator('[name="repair[description]"]').fill(`Anello del sollecito 085 ${vista}\nseconda riga`);
  await page.locator('[name="repair[accepted_on]"]').fill('2025-06-04');
  await page.locator('[name="repair[qr_code]"]').fill(codice);
  await numeroDel2025(page, pc ? '8086/25' : '8087/25');
  await page.locator('#new-repair-form button[type=submit]').click();
  await page.locator(SEL_INFO, {hasText: SALVATA}).first().waitFor();
  await pronta(page, app);
  o['prepara.messaggio'] = SALVATA.test(await messaggio(page)) ? 'salvata' : await messaggio(page);
  await cercaQR(page, base, app, codice, '/');
  await page.locator('#qr-btn-consulta').click();
  await page.waitForURL(/\/repairs\/[0-9]+/);
  await pronta(page, app);
  const busta = Number(percorso(page).split('/').pop());
  fa('senza-artigiano');
  o['senza-artigiano.pulsante'] = String(await page.locator('#repair-artisan-notify').count());
  fa('affida');
  await page.locator('#edit-repair').click();
  await page.locator('#repair_artisan_id').waitFor({state: 'visible'});
  await page.locator('#repair_artisan_id').selectOption({label: nome});
  await page.locator('#save-repair').click();
  await page.locator('#repair-artisan', {hasText: nome}).waitFor();
  await pronta(page, app);
  const prima = await contaRegistro(page, base, 'outgoing.handed_off');
  fa('scheda');
  await apriScheda(page, base, app, busta);
  o['scheda.pulsante'] = await leggi(page, '#repair-artisan-notify');
  o['scheda.senza-telefono'] = String(await page.locator('#repair-artisan-send-missing').count());
  fa('popup');
  await page.locator('#repair-artisan-notify').click();
  const via = await leggiPopup(page, o, 'popup');
  o['popup.percorso'] = percorso(page).replace(/[0-9]+/, 'N');
  fa('annulla');
  await page.locator('#notice-cancel').click();
  await page.locator('#notice-popup').waitFor({state: 'detached'});
  await pronta(page, app);
  o['annulla.percorso'] = percorso(page).replace(/[0-9]+/, 'N');
  fa('invio');
  await page.locator('#repair-artisan-notify').click();
  await page.locator('#notice-popup').waitFor();
  await page.locator('#notice-body').fill(`Sollecito del banco 085 (${vista}), senza link`);
  await inviaPopup(page, o, 'invio', via);
  fa('lista');
  await apriScheda(page, base, app, busta);
  const lista = page.locator('#repair-send-history');
  o['lista.titolo'] = (await lista.count()) ? testo(await lista.locator('summary').innerText()) : NESSUNO;
  if (await lista.count()) {
    await lista.locator('summary').click();
    o['lista.riga'] = testo(await lista.locator('li[id^="repair-send-message-"]').first().innerText()).replace(DATA_ORA, '<data>');
  }
  o['registro.outgoing-handed-off'] = String((await contaRegistro(page, base, 'outgoing.handed_off')) - prima);
  fa('fine');
  await apriScheda(page, base, app, busta);
  await page.locator('#edit-repair').click();
  await page.locator('#repair_artisan_id').waitFor({state: 'visible'});
  await page.locator('#repair_artisan_id').selectOption('');
  await page.locator('#save-repair').click();
  await page.locator('#repair-artisan', {hasText: '—'}).waitFor();
  await pronta(page, app);
  o['fine.pulsante'] = String(await page.locator('#repair-artisan-notify').count());
  await anagrafica();
  finestra = NESSUNO;
  await (await rigaArtigiano()).locator('[id^="delete-artisan-"]').click();
  await scrittaInPagina('Artigiano eliminato.');
  o['fine.eliminato'] = String((await rigaArtigiano()) === null);
}

// assistente-salva (slice 107, ADM-11): la pagina «Assistente e dettatura» del titolare, parte configurazione, nelle due app, con
// il modello finto della 106 (container del banco con l'alias modello.invalid, solo con i dati: MODELLO_URL). Gira IN FONDO,
// dopo tutti gli altri scenari e dopo lettore-qr (main, sotto), perché un salvataggio riuscito NON SI PUÒ DISFARE: lo
// standalone rifiuta un indirizzo o un modello vuoti («non può essere vuoto», validate_required), quindi dopo lo scenario le
// due app restano configurate sul modello finto e la pagina di accettazione dello standalone mostrerebbe il riquadro del
// dettato. Per vista, prima il telefono (la pagina com'è) e poi il PC. Sul PC, sempre: quattro salvataggi rifiutati (schema
// sbagliato e modello vuoto; chiave in chiaro verso modello.invalid, che non è questa macchina; indirizzo senza server e
// modello di 121 caratteri; trascrizione senza indirizzo e glossario di 2001 caratteri), che non scrivono niente, e la prova
// senza configurazione. Solo con i dati: l'estrazione salvata sul modello finto, la prova con una risposta programmata (la
// descrizione sia in «d», il formato corto del Go, D-103, sia in «descrizione», quello lungo dello standalone: le due app
// leggono la stessa frase) e con due 500 (lo standalone ne consuma uno, il Go due: D-19), la trascrizione con il glossario,
// una chiave salvata verso localhost e poi cancellata; alla fine (finally) rimette quello che si può: chiavi cancellate,
// glossario vuoto, estrazione e trascrizione sul modello finto. Le righe assist.provider_configured e assist.extract si
// contano prima e dopo. CHIAVE_BANCO non va mai su una pagina.
const CHIAVE_BANCO = ['chiave', 'finta', 'banco', '107'].join('-');
const FRASE_PROVA = 'Busta 1, anello in oro da stringere di una misura, preventivo 40 euro.';

async function modelloFinto(percorso, corpo) {
  const base = process.env.MODELLO_URL || '';
  if (!/^http:\/\/[a-z0-9.-]+\.invalid:[0-9]+$/.test(base)) throw new Error('modello finto assente (MODELLO_URL)');
  const r = await fetch(base + percorso, corpo === undefined ? {} :
    {method: 'POST', headers: {'content-type': 'application/json'}, body: JSON.stringify(corpo)});
  if (!r.ok) throw new Error(`modello finto: ${r.status}`);
  return r.json();
}

async function assistente(page, {base, app, vista, dati}, o) {
  page.__app = app;
  const MODELLO = process.env.MODELLO_URL || '';
  const apri = async () => { await page.goto(`${base}/settings/assistente`); await pronta(page, app); };
  // La sezione del riquadro è la più interna che contiene il modulo: la pagina sta dentro #settings-panel, anch'essa una
  // section (base.html), e lo standalone non ha gli id ai-card e stt-card.
  const riquadro = f => page.locator(`section:has(#${f}-form):not(:has(section))`);
  const stato = async f => testo(await riquadro(f).locator('h2 + span').first().innerText());
  const errori = async id => (await page.locator(`#${id}`).locator('xpath=ancestor::div[1]').locator('p.text-error, p.field-error')
    .allInnerTexts()).map(testo).join(' | ') || NESSUNO;
  const CAMPI = {ai: ['endpoint', 'model', 'api_key'], stt: ['endpoint', 'api_key', 'glossary']};
  const valori = async f => {
    const v = {};
    for (const c of CAMPI[f]) v[c] = await page.locator(`#${f}_${c}`).inputValue();
    return JSON.stringify(v);
  };
  const segnaposto = async f => JSON.stringify(await page.locator(`#${f}_api_key`).getAttribute('placeholder'));
  const compila = async (f, v) => { for (const [c, x] of Object.entries(v)) await page.locator(`#${f}_${c}`).fill(x); };
  const registro = async () => {
    const n = {};
    for (const a of ['assist.provider_configured', 'assist.extract']) n[a] = await contaRegistro(page, base, a);
    return n;
  };
  // Come provider-posta: nello standalone LiveView ridisegna sul posto, il Go torna la pagina; si aspettano errori del modulo
  // diversi da quelli di prima, o il messaggio.
  // «rifiuto-muto» (rifiuto-2, D-107): lo standalone rifiuta la chiave in chiaro senza mostrare niente sotto le caselle
  // (blank_secret, nota 10 di «Note per lo standalone»): da lui non arriva nessun errore, spariscono solo quelli del rifiuto
  // di prima. Lì basta che gli errori cambino, anche in nessuno, e che LiveView abbia chiuso il submit (il modulo perde
  // phx-submit-loading). Serve un rifiuto di prima ancora sulla pagina, se no «cambiare» non si vede. Il Go resta come
  // «errore»: il messaggio deve arrivare, e se non arriva è un ERRORE del giro, non una pagina vuota che passa.
  const salva = async (f, attesa) => {
    const SEL = `#${f}-form p.text-error, #${f}-form p.field-error`;
    const prima = (await page.locator(SEL).allInnerTexts()).join('|');
    const muto = attesa === 'rifiuto-muto' && app === 'standalone';
    if (attesa === 'rifiuto-muto' && prima === '') throw new Error(`rifiuto muto del modulo ${f} senza errori di prima`);
    await page.evaluate(id => { document.getElementById(id).noValidate = true; }, `${f}-form`);
    await page.locator(`#${f}-form button`).click();
    if (attesa === 'errore' || attesa === 'rifiuto-muto') {
      const fine = Date.now() + 30000;
      for (;;) {
        const ora = (await page.locator(SEL).allInnerTexts().catch(() => [])).join('|');
        if (ora !== prima && (ora !== '' || muto)) break;
        if (Date.now() > fine) throw new Error(`errori del modulo ${f} non arrivati`);
        await page.waitForTimeout(200);
      }
      if (muto) await page.locator(`#${f}-form.phx-submit-loading`).waitFor({state: 'detached', timeout: 30000});
    } else await page.waitForFunction(() => /Impostazione salvata/.test(document.body ? document.body.innerText : ''));
    await pronta(page, app);
  };
  const leggiProva = async () => (await page.locator('#ai-probe').count()) ? testo(await page.locator('#ai-probe').innerText()) : '';
  // La prova: l'esito sotto il pulsante (nello standalone sul posto, nel Go nella pagina che torna), diverso da quello di prima.
  const prova = async k => {
    const prima = await leggiProva();
    await page.locator('#ai-test').click();
    const fine = Date.now() + 100000;
    for (;;) {
      const ora = await leggiProva().catch(() => '');
      if (ora !== '' && ora !== prima) break;
      if (Date.now() > fine) throw new Error('esito della prova non arrivato');
      await page.waitForTimeout(200);
    }
    await pronta(page, app);
    o[`${k}.esito`] = await leggiProva();
    o[`${k}.errore`] = String(await page.locator('#ai-probe').evaluate(e => e.classList.contains('text-error')));
  };
  const inPagina = async () => String((await page.content()).includes(CHIAVE_BANCO));
  fa('pagina');
  await apri();
  o['pagina.titolo'] = testo(await page.locator('h1').first().innerText());
  o['pagina.intro'] = testo(await page.locator('.max-w-prose').first().innerText());
  for (const f of ['ai', 'stt']) {
    o[`pagina.${f}.stato`] = await stato(f);
    o[`pagina.${f}.titolo`] = testo(await riquadro(f).locator('h2').first().innerText());
    o[`pagina.${f}.etichette`] = (await page.locator(`#${f}-form label`).allInnerTexts()).map(testo).join(' | ');
    o[`pagina.${f}.campi`] = (await page.locator(`#${f}-form [name^="${f}["]`).evaluateAll(els => els.map(e =>
      `${e.name}:${e.type}:${e.getAttribute('placeholder') ?? '-'}`))).join(' | ');
    o[`pagina.${f}.valori`] = await valori(f);
    o[`pagina.${f}.segnaposto`] = await segnaposto(f);
    o[`pagina.${f}.pulsante`] = testo(await page.locator(`#${f}-form button`).innerText());
  }
  o['pagina.aiuto-glossario'] = testo(await page.locator('#stt-form > p').last().innerText());
  o['pagina.prova'] = testo(await page.locator('#ai-test').innerText());
  o['pagina.esito'] = (await page.locator('#ai-probe').count()) ? 'presente' : 'assente';
  if (vista !== 'desktop') return;
  const prima = await registro();
  fa('rifiuto-1');
  await apri();
  await compila('ai', {endpoint: 'ftp://modello.invalid', model: '', api_key: ''});
  await salva('ai', 'errore');
  for (const c of CAMPI.ai) o[`rifiuto-1.errori.${c.replace("_", "-")}`] = await errori(`ai_${c}`);
  o['rifiuto-1.valori'] = await valori('ai');
  o['rifiuto-1.stato'] = await stato('ai');
  fa('rifiuto-2');
  await compila('ai', {endpoint: MODELLO || 'http://modello.invalid:8082', model: 'modello-finto', api_key: CHIAVE_BANCO});
  await salva('ai', 'rifiuto-muto');
  for (const c of CAMPI.ai) o[`rifiuto-2.errori.${c.replace("_", "-")}`] = await errori(`ai_${c}`);
  o['rifiuto-2.chiave'] = JSON.stringify(await page.locator('#ai_api_key').inputValue());
  o['rifiuto-2.chiave-in-pagina'] = await inPagina();
  fa('rifiuto-3');
  await compila('ai', {endpoint: 'http://', model: 'm'.repeat(121), api_key: ''});
  await salva('ai', 'errore');
  for (const c of CAMPI.ai) o[`rifiuto-3.errori.${c.replace("_", "-")}`] = await errori(`ai_${c}`);
  fa('rifiuto-4');
  await apri();
  await compila('stt', {endpoint: '', glossary: 'a'.repeat(2001)});
  await salva('stt', 'errore');
  for (const c of CAMPI.stt) o[`rifiuto-4.errori.${c.replace("_", "-")}`] = await errori(`stt_${c}`);
  o['rifiuto-4.glossario'] = String((await page.locator('#stt_glossary').inputValue()).length);
  fa('prova-vuota');
  await apri();
  o['prova-vuota.stati'] = `${await stato('ai')} | ${await stato('stt')}`;
  await prova('prova-vuota');
  if (!dati) {
    fa('registro');
    const dopo = await registro();
    for (const a of Object.keys(dopo)) o[`registro.${a.replace(/[._]/g, '-')}`] = String(dopo[a] - prima[a]);
    return;
  }
  let erroreRipristino = NESSUNO;
  try {
    fa('salva-ai');
    await apri();
    await compila('ai', {endpoint: MODELLO, model: 'modello-finto', api_key: ''});
    await salva('ai', 'salvato');
    o['salva-ai.messaggio'] = await messaggio(page);
    o['salva-ai.stato'] = await stato('ai');
    o['salva-ai.segnaposto'] = await segnaposto('ai');
    await apri();
    o['salva-ai.valori'] = (await valori('ai')).replace(MODELLO, '<modello>');
    fa('prova-buona');
    const descrizione = 'anello in oro da stringere di una misura';
    await modelloFinto('/_prova/risposte', [{contenuto: JSON.stringify({d: descrizione, descrizione, p: '40', preventivo: '40'})}]);
    const richieste = (await modelloFinto('/_prova/richieste')).totale;
    await prova('prova-buona');
    const r = await modelloFinto('/_prova/richieste');
    o['prova-buona.richieste'] = String(r.totale - richieste);
    o['prova-buona.autorizzazione'] = String(!!(r.ultima && r.ultima.con_autorizz));
    const messaggi = (r.ultima && r.ultima.corpo && r.ultima.corpo.messages) || [];
    o['prova-buona.frase'] = String(messaggi.some(m => m.role === 'user' && String(m.content).includes(FRASE_PROVA)));
    fa('prova-500');
    await modelloFinto('/_prova/risposte', [{stato: 500}, {stato: 500}]);
    await prova('prova-500');
    await modelloFinto('/_prova/risposte', []);
    fa('salva-stt');
    await apri();
    await compila('stt', {endpoint: MODELLO, api_key: '', glossary: 'Poloni, Bottari\n  Gioielli   Rossi\n\n'});
    await salva('stt', 'salvato');
    o['salva-stt.stato'] = await stato('stt');
    await apri();
    o['salva-stt.glossario'] = JSON.stringify(await page.locator('#stt_glossary').inputValue());
    fa('chiave');
    await compila('ai', {endpoint: 'http://localhost:8082', api_key: CHIAVE_BANCO});
    await salva('ai', 'salvato');
    await apri();
    o['chiave.segnaposto'] = await segnaposto('ai');
    o['chiave.valore'] = JSON.stringify(await page.locator('#ai_api_key').inputValue());
    o['chiave.in-pagina'] = await inPagina();
    fa('cancella');
    await compila('ai', {endpoint: MODELLO});
    await page.locator('input[name="ai[clear_secret]"]').check();
    await salva('ai', 'salvato');
    await apri();
    o['cancella.segnaposto'] = await segnaposto('ai');
    o['cancella.stato'] = await stato('ai');
  } finally {
    // Rimette quello che si può: niente chiavi, niente glossario, i due indirizzi sul modello finto.
    try {
      fa('ripristino');
      await modelloFinto('/_prova/risposte', []).catch(() => {});
      await apri();
      await compila('ai', {endpoint: MODELLO, model: 'modello-finto', api_key: ''});
      await page.locator('input[name="ai[clear_secret]"]').check();
      await salva('ai', 'salvato');
      await apri();
      await compila('stt', {endpoint: MODELLO, api_key: '', glossary: ''});
      await page.locator('input[name="stt[clear_secret]"]').check();
      await salva('stt', 'salvato');
    } catch (e) {
      erroreRipristino = String(e && e.message || e).split('\n')[0].replace(/https:\/\/[^\s/]+/g, '<app>');
    }
  }
  await apri();
  o['ripristino.errore'] = erroreRipristino;
  o['ripristino.ai'] = (await valori('ai')).replace(MODELLO, '<modello>');
  o['ripristino.stt'] = (await valori('stt')).replace(MODELLO, '<modello>');
  o['ripristino.segnaposti'] = `${await segnaposto('ai')} | ${await segnaposto('stt')}`;
  fa('registro');
  const dopo = await registro();
  for (const a of Object.keys(dopo)) o[`registro.${a.replace(/[._]/g, '-')}`] = String(dopo[a] - prima[a]);
}

// Il «Dettato AI» della busta nuova (slice 108; RIP-H-42, RIP-H-43): gira in fondo dopo assistente-salva, che lascia le due
// app con l'estrazione sul modello finto (con i dati; senza, le due app non sono configurate e si guarda solo «🔒 Dettato AI»).
// Il riquadro nelle due viste; sul PC un rifiuto (testo corto) che non tocca il modulo aperto e scritto a mano, poi
// un'estrazione con una risposta programmata che porta le chiavi dei due formati (corto del Go, D-103, e lungo dello
// standalone), così le differenze sono solo quelle volute: la consegna calcolata dalla frase «c» (D-18) e «Busta N — Acconto
// N €» nelle note (D-20). Infine la busta propria del 2025 (8108/25 nello standalone, «SINT-DETTATO-108-desktop») si salva.
// Il microfono dello standalone (la trascrizione resta configurata da assistente-salva) arriva nel Go con le 109-110: non
// si guarda.
async function dettatoNuova(page, {base, app, vista, dati}, o) {
  page.__app = app;
  const apri = async () => { await page.goto(`${base}/nuova`); await pronta(page, app); };
  const valore = async sel => (await page.locator(sel).count()) ? await page.locator(sel).inputValue() : NESSUNO;
  const stato = async () => (await page.locator('#assist-status').count()) ? testo(await page.locator('#assist-status').innerText()) : NESSUNO;
  const aperto = async () => String(await page.locator('#manual-intake').evaluate(e => e.open));
  // Nello standalone LiveView scrive il messaggio sul posto, il Go torna la pagina: si aspetta il messaggio voluto.
  const estrai = async (testoDettato, atteso) => {
    await page.locator('#assist-text').fill(testoDettato);
    await page.locator('#assist-estrai').click();
    const fine = Date.now() + 100000;
    for (;;) {
      const ora = await stato().catch(() => '');
      if (atteso.test(ora)) break;
      if (Date.now() > fine) throw new Error('messaggio del dettato non arrivato');
      await page.waitForTimeout(200);
    }
    await pronta(page, app);
  };
  fa('pagina');
  await apri();
  o['pagina.disattivato'] = String(await page.locator('#assist-disabled').count());
  o['pagina.riquadro'] = String(await page.locator('#assist-card').count());
  // Il primo span del sommario: il secondo è la freccia «⌄».
  o['pagina.manuale'] = testo(await page.locator('#manual-intake > summary > span:first-child').innerText());
  o['pagina.aperto'] = await aperto();
  if (!dati) return;
  o['pagina.titolo'] = testo(await page.locator('#assist-card h2').first().innerText());
  o['pagina.etichetta'] = testo(await page.locator('label[for="assist-text"]').innerText());
  o['pagina.segnaposto'] = String(await page.locator('#assist-text').getAttribute('placeholder'));
  o['pagina.pulsante'] = testo(await page.locator('#assist-estrai').innerText());
  o['pagina.stato'] = await stato() || NESSUNO;
  if (vista !== 'desktop') return;
  const prima = await contaRegistro(page, base, 'assist.extract');
  fa('rifiuto');
  await apri();
  await page.locator('#manual-intake > summary').click();
  await page.locator('[name="repair[customer_name]"]').waitFor({state: 'visible'});
  await pronta(page, app);
  await page.locator('[name="repair[customer_name]"]').fill('Nome scritto a mano 108');
  await estrai('corto', /Testo troppo corto/);
  o['rifiuto.stato'] = await stato();
  o['rifiuto.errore'] = String(await page.locator('#assist-status').evaluate(e => e.classList.contains('text-error')));
  o['rifiuto.nome'] = await valore('[name="repair[customer_name]"]');
  o['rifiuto.aperto'] = await aperto();
  o['rifiuto.casella'] = await valore('#assist-text');
  fa('estrai');
  const oggi = await valore('[name="repair[accepted_on]"]');
  const nome = 'Ofelia Dettato';
  const dettato = `Cliente ${nome} 3330108108, anello con pietra da ridimensionare, anche pulizia, busta 31 acconto 20 euro, consegna fra due settimane`;
  await modelloFinto('/_prova/risposte', [{contenuto: JSON.stringify({
    n: nome, o: '', b: 31, d: 'anello con pietra da ridimensionare', c: 'fra 2 settimane', p: '120', a: 20, x: 'anche pulizia',
    nome, telefono: '3330108108', oreficeria: '', descrizione: 'anello con pietra da ridimensionare', note: 'anche pulizia',
    preventivo: '120', data: '', data_consegna: '', compro_oro: 0})}]);
  await estrai(dettato, /Dati estratti/);
  await modelloFinto('/_prova/risposte', []);
  o['estratto.stato'] = await stato();
  o['estratto.errore'] = String(await page.locator('#assist-status').evaluate(e => e.classList.contains('text-error')));
  o['estratto.aperto'] = await aperto();
  o['estratto.casella'] = String((await valore('#assist-text')) === dettato);
  o['estratto.nome'] = await valore('[name="repair[customer_name]"]');
  o['estratto.telefono'] = await valore('[name="repair[customer_phone]"]');
  o['estratto.oreficeria'] = JSON.stringify(await valore('[name="repair[oreficeria_id]"]'));
  o['estratto.artigiano'] = JSON.stringify(await valore('[name="repair[artisan_id]"]'));
  const ricezione = await valore('[name="repair[accepted_on]"]');
  o['estratto.ricezione'] = ricezione === oggi ? 'oggi' : ricezione;
  // La consegna come giorni da oggi (il giorno del giro cambia): «(vuota)» se non c'è.
  const consegna = await valore('[name="repair[expected_on]"]');
  o['estratto.consegna'] = consegna ? `oggi+${Math.round((Date.parse(consegna) - Date.parse(oggi)) / 86400000)}` : '(vuota)';
  o['estratto.descrizione'] = await valore('[name="repair[description]"]');
  o['estratto.note'] = await valore('[name="repair[public_notes]"]');
  o['estratto.note-interne'] = JSON.stringify(await valore('[name="repair[internal_notes]"]'));
  o['estratto.preventivo'] = await valore('[name="repair[estimate_euros]"]');
  o['estratto.compro-oro'] = String(await page.locator('#repair_compro_oro').isChecked());
  o['estratto.qr'] = JSON.stringify(await valore('[name="repair[qr_code]"]'));
  fa('salva');
  const codice = `SINT-DETTATO-108-${vista}`;
  await page.locator('[name="repair[accepted_on]"]').fill('2025-06-08');
  await page.locator('[name="repair[qr_code]"]').fill(codice);
  await numeroDel2025(page, '8108/25');
  await page.locator('#new-repair-form button[type=submit]').click();
  await page.locator(SEL_INFO, {hasText: SALVATA}).first().waitFor();
  await pronta(page, app);
  o['salva.messaggio'] = SALVATA.test(await messaggio(page)) ? 'salvata' : await messaggio(page);
  await cercaQR(page, base, app, codice, '/');
  await page.locator('#qr-btn-consulta').click();
  await page.waitForURL(/\/repairs\/[0-9]+/);
  await pronta(page, app);
  o['scheda.cliente'] = String((await page.locator('body').innerText()).includes(nome));
  o['scheda.preventivo'] = await leggi(page, '#repair-estimate');
  fa('registro');
  o['registro.assist-extract'] = String(await contaRegistro(page, base, 'assist.extract') - prima);
}

// --- «🎤 Detta a voce» (slice 110; RIP-H-44, D-25) ------------------------------------------------
//
// Due scenari in fondo, dopo assistente-salva, che lascia le due app con la trascrizione puntata sul trascrittore
// finto (MODELLO_URL, POST /stt; slice 109). Senza i dati le app non sono configurate e si guarda solo che il
// microfono non c'è. Per vista, il pulsante e il campo della clip; sul PC:
//   - dettato-clip (il confronto del caricamento di una clip, spostato dalla 109): un file sintetico sul campo file del
//     riquadro (nello standalone il campo dell'upload LiveView, nel Go #assist-clip della 110: tutti e due in
//     #assist-form), uno per caso, ognuno su una pagina nuova: troppo corta (sotto i 2000 byte), un formato che non si
//     inoltra (testo), niente voce (il finto risponde ""), il trascrittore guasto (500) e la riuscita in una casella
//     scritta a mano con gli spazi ai bordi (append_dictated/2). Si guardano il messaggio, il rosso, la casella, le
//     richieste arrivate al finto e il loro tipo, e le righe assist.transcribe;
//   - dettato-microfono, con il microfono finto di Chromium (microfono.wav di fotocamera.go: un tono a 440 Hz): un
//     tocco breve («⚠ Troppo breve, riprova.», nessuna richiesta), una registrazione di 2 s in una casella scritta a
//     mano (il testo programmato in fondo; la clip arrivata al finto: webm nello standalone, WAV mono a 16 kHz con il
//     tono del microfono nel Go, D-25), il microfono negato, il contesto non sicuro e un browser senza microfono (questi
//     tre cambiando la pagina prima del tocco, come li vedrebbe il hook).
const TESTO_CLIP = 'anello da lucidare, busta 110';
const CASELLA_CLIP = '  Scritto a mano 110  ';
const SECONDA_RIPRESA = 'seconda ripresa 110';

// wavInventato è un WAV PCM mono a 16 kHz di secondi secondi con un tono a 440 Hz: un suono inventato, nessuna voce.
function wavInventato(secondi) {
  const n = Math.round(16000 * secondi), b = Buffer.alloc(44 + 2 * n);
  b.write('RIFF', 0); b.writeUInt32LE(36 + 2 * n, 4); b.write('WAVEfmt ', 8); b.writeUInt32LE(16, 16); b.writeUInt16LE(1, 20);
  b.writeUInt16LE(1, 22); b.writeUInt32LE(16000, 24); b.writeUInt32LE(32000, 28); b.writeUInt16LE(2, 32); b.writeUInt16LE(16, 34);
  b.write('data', 36); b.writeUInt32LE(2 * n, 40);
  for (let i = 0; i < n; i++) b.writeInt16LE(Math.round(9000 * Math.sin(2 * Math.PI * 440 * i / 16000)), 44 + 2 * i);
  return b;
}

// Il riquadro del dettato della pagina nuova, uguale per i due scenari.
function dettatoVoce(page, base, app) {
  const stato = async () => (await page.locator('#assist-status').count()) ? testo(await page.locator('#assist-status').innerText()) : NESSUNO;
  return {
    apri: async () => { await page.goto(`${base}/nuova`); await pronta(page, app); },
    stato,
    rosso: async () => String(await page.locator('#assist-status').evaluate(e => e.classList.contains('text-error'))),
    casella: async () => JSON.stringify(await page.locator('#assist-text').inputValue()),
    // Il messaggio finale: «⏳ Trascrivo…» (Go) è solo di passaggio.
    aspetta: async atteso => {
      const fine = Date.now() + 60000;
      for (;;) {
        if (atteso.test(await stato().catch(() => ''))) return;
        if (Date.now() > fine) throw new Error('messaggio del microfono non arrivato');
        await page.waitForTimeout(200);
      }
    },
    richieste: async () => (await modelloFinto('/_prova/richieste-stt')).totale,
    pagina: async o => {
      o['pagina.microfono'] = String(await page.locator('#assist-mic').count());
      o['pagina.campo-clip'] = String(await page.locator('#assist-form input[type=file]').count());
    },
  };
}

// clipArrivata descrive l'ultima clip arrivata al trascrittore finto: il tipo e, per un WAV, com'è fatto e il tono.
async function clipArrivata() {
  const u = (await modelloFinto('/_prova/richieste-stt')).ultima || {};
  const tono = u.tono_hz ? `tono di ${Math.abs(u.tono_hz - 440) <= 40 ? '440 Hz circa' : `${u.tono_hz} Hz`}` : '';
  return [u.content_type || NESSUNO, u.wav, tono].filter(Boolean).join(', ');
}

async function dettatoClip(page, {base, app, vista, dati}, o) {
  page.__app = app;
  const d = dettatoVoce(page, base, app);
  fa('pagina');
  await d.apri();
  await d.pagina(o);
  if (!dati || vista !== 'desktop') return;
  const prima = await contaRegistro(page, base, 'assist.transcribe');
  const casi = [
    {k: 'corta', file: {name: 'corta-110.wav', mimeType: 'audio/wav', buffer: wavInventato(0.02)}, atteso: /troppo breve/},
    {k: 'formato', file: {name: 'nota-110.txt', mimeType: 'text/plain', buffer: Buffer.alloc(3000, 0x2b)}, atteso: /formato/},
    {k: 'silenzio', stt: [{contenuto: ''}], file: {name: 'zitto-110.wav', mimeType: 'audio/wav', buffer: wavInventato(0.5)}, atteso: /Non ho sentito/},
    {k: 'guasto', stt: [{stato: 500}], file: {name: 'guasto-110.wav', mimeType: 'audio/wav', buffer: wavInventato(0.5)}, atteso: /non disponibile/},
    {k: 'riuscita', casella: CASELLA_CLIP, stt: [{contenuto: TESTO_CLIP}], file: {name: 'clip-110.wav', mimeType: 'audio/wav', buffer: wavInventato(0.5)}, atteso: /Trascritto/},
  ];
  try {
    for (const c of casi) {
      fa(c.k);
      await d.apri();
      // La casella senza il cursore: LiveView non riscrive una casella che ha il fuoco (revisione della 110, M1).
      if (c.casella) { await page.locator('#assist-text').fill(c.casella); await page.locator('#assist-text').blur(); }
      await modelloFinto('/_prova/risposte-stt', c.stt || []);
      const r = await d.richieste();
      await page.locator('#assist-form input[type=file]').setInputFiles(c.file);
      await d.aspetta(c.atteso);
      o[`${c.k}.stato`] = await d.stato();
      o[`${c.k}.errore`] = await d.rosso();
      o[`${c.k}.casella`] = await d.casella();
      o[`${c.k}.richieste`] = String(await d.richieste() - r);
      if (await d.richieste() > r) o[`${c.k}.clip`] = await clipArrivata();
    }
  } finally {
    await modelloFinto('/_prova/risposte-stt', []).catch(() => {});
  }
  fa('registro');
  o['registro.assist-transcribe'] = String(await contaRegistro(page, base, 'assist.transcribe') - prima);
}

// osservaMicrofono si mette nella pagina prima dei suoi script (revisione della 110, M3): tiene i flussi dati da
// getUserMedia e i contesti audio creati, per vedere a fine caso che il microfono è spento e i contesti chiusi.
function osservaMicrofono() {
  window.__flussiBanco = [];
  window.__contestiBanco = [];
  const md = navigator.mediaDevices;
  if (md && md.getUserMedia) {
    const g = md.getUserMedia.bind(md);
    md.getUserMedia = c => g(c).then(f => { window.__flussiBanco.push(f); return f; });
  }
  const C = window.AudioContext;
  if (C) window.AudioContext = class extends C { constructor(...a) { super(...a); window.__contestiBanco.push(this); } };
}

// rilascio dice se il microfono è stato rilasciato: tutte le tracce ended e tutti i contesti closed (entro 3 s).
async function rilascio(page) {
  const leggi = () => page.evaluate(() => ({
    tracce: (window.__flussiBanco || []).flatMap(f => f.getTracks()).filter(t => t.readyState !== 'ended').length,
    contesti: (window.__contestiBanco || []).filter(c => c.state !== 'closed').length}));
  const fine = Date.now() + 3000;
  for (;;) {
    const r = await leggi();
    if (r.tracce === 0 && r.contesti === 0) return 'rilasciato';
    if (Date.now() > fine) return `tracce accese ${r.tracce}, contesti aperti ${r.contesti}`;
    await page.waitForTimeout(200);
  }
}

async function dettatoMicrofono(page, {base, app, vista, dati}, o) {
  page.__app = app;
  await page.addInitScript(osservaMicrofono);
  const d = dettatoVoce(page, base, app);
  const mic = page.locator('#assist-mic');
  const pulsante = async () => testo(await mic.innerText());
  // registra: un tocco, il pulsante in ascolto, ms di registrazione, il secondo tocco.
  const registra = async ms => {
    await mic.click();
    await page.locator('#assist-mic', {hasText: 'Sto ascoltando'}).waitFor();
    if (ms) await page.waitForTimeout(ms);
    await mic.click();
  };
  fa('pagina');
  await d.apri();
  await d.pagina(o);
  if (!dati) return;
  o['pagina.pulsante'] = await pulsante();
  o['pagina.etichetta'] = String(await mic.getAttribute('aria-label'));
  if (vista !== 'desktop') return;
  const prima = await contaRegistro(page, base, 'assist.transcribe');
  fa('breve');
  await d.apri();
  let r = await d.richieste();
  await mic.click();
  await page.locator('#assist-mic', {hasText: 'Sto ascoltando'}).waitFor();
  o['ascolto.pulsante'] = await pulsante();
  await mic.click();
  await d.aspetta(/Troppo breve/);
  o['breve.stato'] = await d.stato();
  o['breve.errore'] = await d.rosso();
  o['breve.pulsante'] = await pulsante();
  o['breve.richieste'] = String(await d.richieste() - r);
  o['breve.rilascio'] = await rilascio(page);
  fa('detta');
  await d.apri();
  await page.locator('#assist-text').fill(CASELLA_CLIP);
  await page.locator('#assist-text').blur();
  try {
    await modelloFinto('/_prova/risposte-stt', [{contenuto: TESTO_CLIP}, {contenuto: SECONDA_RIPRESA}]);
    r = await d.richieste();
    await registra(2000);
    await d.aspetta(/Trascritto/);
    o['detta.stato'] = await d.stato();
    o['detta.errore'] = await d.rosso();
    o['detta.casella'] = await d.casella();
    o['detta.pulsante'] = await pulsante();
    o['detta.richieste'] = String(await d.richieste() - r);
    o['detta.clip'] = await clipArrivata();
    o['detta.rilascio'] = await rilascio(page);
    // Una seconda ripresa sulla stessa pagina (in più riprese, append_dictated/2): il testo dopo il primo.
    fa('due');
    r = await d.richieste();
    await registra(1500);
    const fine = Date.now() + 60000;
    while (!(await page.locator('#assist-text').inputValue()).includes(SECONDA_RIPRESA)) {
      if (Date.now() > fine) throw new Error('seconda ripresa non arrivata');
      await page.waitForTimeout(200);
    }
    await d.aspetta(/Trascritto/);
  } finally {
    await modelloFinto('/_prova/risposte-stt', []).catch(() => {});
  }
  o['due.stato'] = await d.stato();
  o['due.errore'] = await d.rosso();
  o['due.casella'] = await d.casella();
  o['due.pulsante'] = await pulsante();
  o['due.richieste'] = String(await d.richieste() - r);
  o['due.rilascio'] = await rilascio(page);
  // Il microfono negato una volta, poi un tocco sulla stessa pagina: la registrazione riparte (tocco breve, niente invio).
  fa('riprova');
  await d.apri();
  await page.evaluate(() => {
    const md = navigator.mediaDevices, g = md.getUserMedia;
    md.getUserMedia = () => { md.getUserMedia = g; return Promise.reject(new DOMException('Permission denied', 'NotAllowedError')); };
  });
  r = await d.richieste();
  await mic.click();
  await d.aspetta(/negato/);
  o['riprova.negato'] = await d.stato();
  await mic.click();
  await page.locator('#assist-mic', {hasText: 'Sto ascoltando'}).waitFor();
  o['riprova.pulsante'] = await pulsante();
  await mic.click();
  await d.aspetta(/Troppo breve/);
  o['riprova.stato'] = await d.stato();
  o['riprova.richieste'] = String(await d.richieste() - r);
  o['riprova.rilascio'] = await rilascio(page);
  // Il hook guarda il browser al tocco: qui lo si cambia prima, sulla pagina appena aperta.
  for (const [k, cambia, atteso] of [
    ['negato', () => { navigator.mediaDevices.getUserMedia = () => Promise.reject(new DOMException('Permission denied', 'NotAllowedError')); }, /negato/],
    ['insicuro', () => { Object.defineProperty(window, 'isSecureContext', {value: false}); }, /contesto non sicuro/],
    ['senza', () => { Object.defineProperty(navigator, 'mediaDevices', {value: undefined}); }, /dispositivo o browser/],
  ]) {
    fa(k);
    await d.apri();
    await page.evaluate(cambia);
    await mic.click();
    await d.aspetta(atteso);
    o[`${k}.stato`] = await d.stato();
    o[`${k}.errore`] = await d.rosso();
  }
  fa('registro');
  o['registro.assist-transcribe'] = String(await contaRegistro(page, base, 'assist.transcribe') - prima);
}

const SCENARI = {visualizzatore, conferme, caricamento, coda, sfondo, schede, tentativi, 'firma-riquadro': firma, 'pannello-qr': qrPannello,
  'riquadro-azioni': riquadroAzioni, 'galleria-home': galleriaHome, whatsapp, 'testi-invii': testiInvii, 'salva-avverti': salvaAvverti,
  'provider-posta': providerPosta, 'provider-telegram': providerTelegram, 'anagrafica-artigiani': anagraficaArtigiani, 'anagrafica-oreficerie': anagraficaOreficerie, 'profilo-negozio': profiloNegozio,
  'stima-negozio': stimaNegozio, 'lotti-apertura': lottiApertura, 'lotti-buste': lottiBuste, 'lotti-invio': lottiInvio, 'lotti-reinvio': lottiReinvio,
  'sessioni-consegna': sessioniConsegna, 'sessioni-rientro': sessioniRientro, 'storico-correzioni': storicoCorrezioni,
  'consegne-solleciti': consegneSolleciti, 'sollecito-artigiano': sollecitoArtigiano,
  // lettore-qr gira dopo gli altri, con i suoi Chromium (lettore, sopra); assistente per ultimo (IN_FONDO, sotto)
  'lettore-qr': null, 'assistente-salva': null, 'dettato-clip': null, 'dettato-microfono': null, 'dettato-nuova': null};

// IN_FONDO sono gli scenari che girano dopo tutti gli altri, lettore-qr compreso, per ogni app con un contesto per vista
// (prima il telefono, poi il PC): cambiano i dati in un modo che non si può disfare (assistente-salva, slice 107). Girano
// nell'ordine dei nomi: dettato-clip, dettato-microfono (slice 110) e dettato-nuova (slice 108) dopo assistente-salva, che
// lascia le due app sul modello finto e sul trascrittore finto.
const IN_FONDO = {'assistente-salva': assistente, 'dettato-clip': dettatoClip, 'dettato-microfono': dettatoMicrofono, 'dettato-nuova': dettatoNuova};
// ARGOMENTI_IN_FONDO sono le opzioni di Chromium di uno scenario IN_FONDO: dettato-microfono ha il microfono finto (il WAV
// di fotocamera.go; senza la finta richiesta del permesso il Chromium senza finestra risponde «Not supported»).
const ARGOMENTI_IN_FONDO = {'dettato-microfono': ['--use-fake-device-for-media-stream', '--use-fake-ui-for-media-stream',
  `--use-file-for-fake-audio-capture=${FOTOCAMERE}/microfono.wav`]};

// inFondo fa girare lo scenario s di IN_FONDO sull'app nomeApp, con il suo Chromium: un contesto per vista, prima il telefono
// e poi il PC, come gli altri scenari (accesso, guasti, errori senza indirizzi né valori).
async function inFondo(s, app, nomeApp, dati, cred, credDipendente, uscita) {
  const browser = await chromium.launch({headless: true, chromiumSandbox: true, args: ARGOMENTI_IN_FONDO[s] || []});
  try {
    for (const vista of ['telefono', 'desktop']) {
      const base = app[nomeApp];
      const o = {};
      ((uscita.osservazioni[nomeApp] ??= {})[s] ??= {})[vista] = o;
      const ctx = await contesto(browser, base, vista);
      try {
        await accedi(ctx, base, nomeApp, cred);
        const page = await ctx.newPage();
        fa('inizio');
        try {
          await IN_FONDO[s](page, {base, app: nomeApp, vista, dati, cred, credDipendente}, o);
        } catch (e) {
          const riga = String(e && e.message || e).split('\n')[0].replace(/https:\/\/[^\s/]+/g, '<app>');
          uscita.errori.push(`${s} ${vista} ${nomeApp} (fase ${fase}): ${e && e.name}: ${riga}`);
        } finally {
          await page.close().catch(() => {});
          scriviGuasti(ctx, o);
        }
      } catch (e) {
        uscita.errori.push(`${s} ${vista} ${nomeApp} (accesso): ${e && e.name}`);
      } finally {
        await ctx.close().catch(() => {});
      }
    }
  } finally {
    await browser.close().catch(() => {});
  }
}

// --- giro -----------------------------------------------------------------------------------

async function main() {
  const app = {standalone: origine(process.env.STANDALONE_URL), go: origine(process.env.GO_URL)};
  const dati = process.env.DATI === 'sintetici-import';
  if (process.env.DATI !== '' && !dati) throw new Error('DATI sconosciuti');
  const nomi = (process.env.SCENARI || '').split(',').filter(Boolean);
  for (const s of nomi) if (!Object.hasOwn(SCENARI, s)) throw new Error(`scenario browser sconosciuto: ${s}`);
  const cred = {email: await filePrivato('/banco/cred/email'), password: await filePrivato('/banco/cred/password')};
  const credDipendente = {email: await filePrivato('/banco/cred/dipendente_email'),
    password: await filePrivato('/banco/cred/dipendente_password')};
  const uscita = {osservazioni: {}, errori: []};
  const browser = await chromium.launch({headless: true, chromiumSandbox: true});
  try {
    for (const nomeApp of ['standalone', 'go']) {
      uscita.osservazioni[nomeApp] = {};
      for (const vista of Object.keys(VISTE)) {
        const base = app[nomeApp];
        const ctx = await contesto(browser, base, vista);
        try {
          await accedi(ctx, base, nomeApp, cred);
          for (const s of nomi.filter(x => SCENARI[x])) {
            const o = {};
            (uscita.osservazioni[nomeApp][s] ??= {})[vista] = o;
            azzeraGuasti(ctx);
            ctx.trasforma = null;
            ctx.segnali.clear();
            ctx.eventi = [];
            const page = await ctx.newPage();
            fa('inizio');
            try {
              await SCENARI[s](page, {base, app: nomeApp, vista, dati, cred, credDipendente}, o);
            } catch (e) {
              // Solo il nome dell'errore e la riga di Playwright, mai indirizzi completi o valori.
              const riga = String(e && e.message || e).split('\n')[0].replace(/https:\/\/[^\s/]+/g, '<app>');
              uscita.errori.push(`${s} ${vista} ${nomeApp} (fase ${fase}): ${e && e.name}: ${riga}`);
            } finally {
              await page.close().catch(() => {});
              scriviGuasti(ctx, o);
            }
          }
        } catch (e) {
          // Senza il nome di uno scenario davanti: per il confronto è un errore di tutto il giro.
          uscita.errori.push(`accesso ${vista} ${nomeApp}: ${e && e.name}`);
        } finally {
          await ctx.close().catch(() => {});
        }
      }
    }
  } finally {
    await browser.close().catch(() => {});
  }
  if (nomi.includes('lettore-qr')) {
    for (const nomeApp of ['standalone', 'go']) await lettore(app, nomeApp, dati, cred, uscita);
  }
  for (const s of nomi.filter(x => IN_FONDO[x])) {
    for (const nomeApp of ['standalone', 'go']) await inFondo(s, app, nomeApp, dati, cred, credDipendente, uscita);
  }
  await writeFile(process.env.USCITA, JSON.stringify(uscita, null, 2) + '\n', {mode: 0o600});
  for (const e of uscita.errori) console.log('ERRORE', e);
  console.log(`osservazioni scritte, errori ${uscita.errori.length}`);
}

main().catch(e => { console.error('banco browser:', e && e.name, String(e && e.message || e).split('\n')[0]); process.exit(1); });
