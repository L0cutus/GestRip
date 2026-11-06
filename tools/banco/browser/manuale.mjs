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
// nuova con le foto, un foglio di etichette mandato alla coda finta, una firma): l'ordine dell'elenco
// conta. Le foto sono immagini inventate generate qui; le stampanti sono le code finte del banco
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
async function nuovaBustaConFoto(page, base) {
  await vai(page, base, '/nuova');
  await page.locator('#intake-photos').waitFor({state: 'visible'});
  await page.locator('[name="repair[customer_name]"]').fill('Cliente del manuale');
  await page.locator('[name="repair[description]"]').fill('Anello da ridimensionare');
  await page.locator('[name="repair[qr_code]"]').fill('RIP-2026-0900');
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
};

// --- giro -----------------------------------------------------------------------------------

async function main() {
  const base = origine(process.env.GO_URL);
  const elenco = JSON.parse(await readFile(process.env.SCHERMATE, 'utf8')).schermate;
  for (const s of elenco) if (!RICETTE[s.ricetta]) throw new Error(`ricetta sconosciuta: ${s.ricetta}`);
  const cred = {email: await filePrivato('/banco/cred/email'), password: await filePrivato('/banco/cred/password')};
  // --lang: i controlli del browser (il campo file, le date) in italiano, come sul telefono del negozio.
  const browser = await chromium.launch({headless: true, chromiumSandbox: true, args: ['--lang=it-IT']});
  let errori = 0;
  try {
    for (const s of elenco) {
      const {ctx, page} = await contesto(browser, base, s.vista, cred);
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
  }
  if (errori > 0) process.exit(1);
}

main().catch(e => { console.error('manuale:', e && e.name, String(e && e.message || e).split('\n')[0]); process.exit(1); });
