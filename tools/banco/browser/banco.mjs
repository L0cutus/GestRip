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
  // Solo l'origine dell'app sotto prova, come browser/activity-087.mjs dello standalone: ogni altra
  // richiesta si interrompe, i redirect non si seguono da soli e uno verso un'altra origine si
  // interrompe anche lui.
  await ctx.route('**/*', async route => {
    try {
      if (new URL(route.request().url()).origin !== base) { ctx.guasti.fuori++; return await route.abort(); }
      const risposta = await route.fetch({maxRedirects: 0, timeout: 90000});
      const location = risposta.headers().location;
      if (location && new URL(location, route.request().url()).origin !== base) { ctx.guasti.fuori++; return await route.abort(); }
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
  ctx.on('page', page => {
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

  // Tre foto insieme.
  fa('tre');
  await apriScheda(page, base, app, busta);
  const n3 = await numeroFoto(page);
  page.context().segnali.clear();
  await scelta().setInputFiles([foto('a-040a.png', 4), foto('b-040a.png', 5), foto('c-040a.png', 6)]);
  await page.waitForFunction(n => {
    const e = document.querySelector('#repair-photo-upload-errors');
    const h = document.querySelector('#repair-photos h2');
    return (e && e.textContent.trim() !== '') || (h && !h.textContent.includes(`(${n})`));
  }, n3, {timeout: 30000});
  await attendi(6000);
  await pronta(page, app);
  o['tre.foto-in-piu'] = String(await numeroFoto(page) - n3);
  o['tre.errori-visti'] = visti(page, 'errori-foto');
  o['tre.messaggi-visti'] = visti(page, 'messaggio');
  o['tre.errori-finali'] = testo(await page.locator('#repair-photo-upload-errors').innerText()) || NESSUNO;

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

const SCENARI = {visualizzatore, conferme, caricamento};

// --- giro -----------------------------------------------------------------------------------

async function main() {
  const app = {standalone: origine(process.env.STANDALONE_URL), go: origine(process.env.GO_URL)};
  const dati = process.env.DATI === 'sintetici-import';
  if (process.env.DATI !== '' && !dati) throw new Error('DATI sconosciuti');
  const nomi = (process.env.SCENARI || '').split(',').filter(Boolean);
  for (const s of nomi) if (!SCENARI[s]) throw new Error(`scenario browser sconosciuto: ${s}`);
  const cred = {email: await filePrivato('/banco/cred/email'), password: await filePrivato('/banco/cred/password')};
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
          for (const s of nomi) {
            const o = {};
            (uscita.osservazioni[nomeApp][s] ??= {})[vista] = o;
            azzeraGuasti(ctx);
            ctx.segnali.clear();
            ctx.eventi = [];
            const page = await ctx.newPage();
            fa('inizio');
            try {
              await SCENARI[s](page, {base, app: nomeApp, vista, dati}, o);
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
  await writeFile(process.env.USCITA, JSON.stringify(uscita, null, 2) + '\n', {mode: 0o600});
  for (const e of uscita.errori) console.log('ERRORE', e);
  console.log(`osservazioni scritte, errori ${uscita.errori.length}`);
}

main().catch(e => { console.error('banco browser:', e && e.name, String(e && e.message || e).split('\n')[0]); process.exit(1); });
