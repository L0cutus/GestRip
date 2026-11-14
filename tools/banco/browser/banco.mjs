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
    await page.waitForFunction(() => /Testo predefinito ripristinato/.test(document.body.innerText));
  } else {
    await page.locator('#template-text-wa_pronta_compro_oro').fill(nuovo);
    await page.locator('#template-wa_pronta_compro_oro button:not([id])').click();
    await page.waitForFunction(() => /Testo salvato/.test(document.body.innerText));
  }
  await pronta(page, app);
}

// --- azioni del riquadro della lista (slice 056, RIP-H-14, 15, 16, 20) -----------------------------
//
// I pulsanti rapidi, il menu dello stato e il 📌 del riquadro, e «← Indietro» che torna alla lista
// filtrata, nelle due app. Le buste sono fuori da quelle degli altri scenari browser (pannello-qr usa
// la 4/26, 31/26, 32/26, 34/26; lettore-qr la 37/26, 5/26 e 10/26), una serie per vista perché il
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
          senza: letturaSenza, cambia: letturaCambia}[p.prova];
        await prova(page, {base, app: nomeApp, dati}, o, k);
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
    await page.waitForFunction(() => /Testo salvato/.test(document.body.innerText));
    await pronta(page, app);
    o['salva.messaggio'] = await messaggio(page);
    o['salva.casella'] = JSON.stringify(await page.locator(casella).inputValue());
    o['salva.anteprima'] = JSON.stringify(await page.locator('#template-preview-wa_pronta').innerText());
    fa('testi-ripristina');
    await page.locator('#template-restore-wa_pronta').click();
    await page.waitForFunction(() => /Testo predefinito ripristinato/.test(document.body.innerText));
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

// anagrafica-artigiani (slice 076): la parte «Artigiani» di /settings/anagrafiche, nelle due app (lo
// standalone ha in più le oreficerie clienti, che il Go porta con la T4.9: qui non si guardano). Sul database
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
    await page.waitForFunction(x => document.body.innerText.includes(x), t);
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
    else await page.waitForFunction(() => /Provider salvato/.test(document.body.innerText));
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
  await page.waitForFunction(() => /niente è stato accodato/.test(document.body.innerText));
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
    else await page.waitForFunction(() => /Provider salvato/.test(document.body.innerText));
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
    await page.waitForFunction(() => /niente è stato accodato/.test(document.body.innerText));
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

const SCENARI = {visualizzatore, conferme, caricamento, coda, sfondo, schede, tentativi, 'firma-riquadro': firma, 'pannello-qr': qrPannello,
  'riquadro-azioni': riquadroAzioni, 'galleria-home': galleriaHome, whatsapp, 'testi-invii': testiInvii, 'salva-avverti': salvaAvverti,
  'provider-posta': providerPosta, 'provider-telegram': providerTelegram, 'anagrafica-artigiani': anagraficaArtigiani,
  // lettore-qr gira dopo gli altri, con i suoi Chromium (lettore, sopra)
  'lettore-qr': null};

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
  await writeFile(process.env.USCITA, JSON.stringify(uscita, null, 2) + '\n', {mode: 0o600});
  for (const e of uscita.errori) console.log('ERRORE', e);
  console.log(`osservazioni scritte, errori ${uscita.errori.length}`);
}

main().catch(e => { console.error('banco browser:', e && e.name, String(e && e.message || e).split('\n')[0]); process.exit(1); });
