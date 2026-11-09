// Script delle pagine di GestRip: le parti di assets/js/app.js dello standalone che servono al
// guscio. Senza LiveView non c'è altro: le pagine funzionano anche senza questo file, che aggiunge
// solo comodità.

// Slice 162 dello standalone: un tocco fuori chiude ogni menu aperto segnato data-close-outside, ed
// Esc li chiude riportando il fuoco sul suo ☰. I cassetti con dei campi non sono segnati, così un
// tocco sbagliato non perde quello che si è scritto.
document.addEventListener("click", event => {
  document.querySelectorAll("details[data-close-outside][open]").forEach(menu => {
    if (!menu.contains(event.target)) menu.open = false
  })
})
document.addEventListener("keydown", event => {
  if (event.key !== "Escape") return
  document.querySelectorAll("details[data-close-outside][open]").forEach(menu => {
    if (menu.contains(document.activeElement)) menu.querySelector(":scope > summary").focus()
    menu.open = false
  })
})

// Il campo unico della home (slice 052, D-63): quando il campo segnato data-seleziona-tutto prende il
// fuoco il testo già scritto si seleziona, così il lettore a pistola (che scrive come una tastiera) lo
// sostituisce invece di accodarsi («Rossi» + scansione non diventa «RossiRIP-2026-0045»). Dopo il
// fuoco, perché il rilascio del tocco toglierebbe la selezione. Senza JavaScript il campo è uguale.
document.addEventListener("focusin", event => {
  const campo = event.target
  if (!campo.matches || !campo.matches("input[data-seleziona-tutto]")) return
  setTimeout(() => { if (document.activeElement === campo) campo.select() }, 0)
})

// Il suggerimento sotto il campo unico (slice 055d, D-63): con un numero d'etichetta (da 1 a 4 cifre,
// con o senza «/AA», da 1 in su: buste.LeggiRicercaEtichetta) la ricerca non guarda il telefono, e il
// suggerimento non lo dice. Il server sceglie il testo della pagina; qui cambia mentre si scrive.
document.addEventListener("input", event => {
  const campo = event.target
  if (!campo.matches || !campo.matches("#repair-search-code")) return
  const aiuto = document.getElementById("repair-search-hint")
  if (!aiuto) return
  const m = /^([0-9]{1,4})(?:\/[0-9]{2})?$/.exec(campo.value.trim())
  const cifre = m !== null && Number(m[1]) >= 1
  aiuto.textContent = aiuto.dataset[cifre ? "cifre" : "testo"]
})

// Slice 113 dello standalone: i pulsanti flottanti si riducono all'icona mentre la pagina scorre in
// giù e tornano interi risalendo. Lo stato è una classe su <html>.
;(function(){
  let lastY = window.scrollY, ticking = false
  const root = document.documentElement
  function update(){
    const y = window.scrollY
    if (y > 80 && y > lastY) root.classList.add("gs-fabs-shrunk")
    else if (y < lastY || y < 40) root.classList.remove("gs-fabs-shrunk")
    lastY = y
    ticking = false
  }
  window.addEventListener("scroll", function(){
    if (!ticking) { requestAnimationFrame(update); ticking = true }
  }, {passive: true})
})()

// «Togli data» (slice 158 dello standalone): svuota i campi nominati da data-clear-field (id
// separati da spazi: nel modulo di modifica anche il testo di consegna nascosto) e lo dice al
// modulo, perché il selettore delle date dei telefoni non permette di svuotarlo. Nello standalone è
// JS.dispatch("gs:clear-field"); senza JavaScript il pulsante non fa niente, come là.
document.addEventListener("click", event => {
  const button = event.target.closest && event.target.closest("[data-clear-field]")
  if (!button) return
  button.getAttribute("data-clear-field").split(" ").forEach(id => {
    const input = id && document.getElementById(id)
    if (!input) return
    input.value = ""
    input.dispatchEvent(new Event("input", {bubbles: true}))
  })
})

// data-confirm, come phoenix_html dello standalone: prima del clic su un elemento segnato chiede
// conferma con il testo dell'attributo, e «Annulla» ferma il clic, quindi anche l'invio del modulo
// (l'Invio in un campo del modulo è un clic sul pulsante). Senza JavaScript il pulsante invia senza
// chiedere, tranne «✓ Consegnata» del riquadro (slice 060): il suo modulo ha il campo conferma
// (data-conferma-js), vuoto senza JavaScript, e il server disegna la conferma; qui, dopo «OK», vale 1
// e il server non chiede di nuovo. Sta prima di data-invio-unico, che spegne il pulsante solo a invio
// partito.
document.addEventListener("click", event => {
  const el = event.target.closest && event.target.closest("[data-confirm]")
  if (!el) return
  if (!window.confirm(el.getAttribute("data-confirm"))) {
    event.preventDefault()
    event.stopImmediatePropagation()
    return
  }
  const campo = el.form && el.form.querySelector("input[data-conferma-js]")
  if (campo) campo.value = "1"
})

// Il pannello del QR (slice 054, D-70): la conferma di «#qr-btn-avanza» (data-qr-chiedi) si apre al
// posto dei pulsanti, senza passare dal server, come la conferma in posto dello standalone
// (#qr-action-confirm con «Sì» e «Annulla»): «Sì» manda il modulo con conferma=1, «Annulla» torna ai
// pulsanti. Senza JavaScript il pulsante manda il modulo senza conferma e la conferma la disegna il
// server; su quella pagina (senza i pulsanti) «Annulla» va al server, che ridisegna il pannello.
document.addEventListener("click", event => {
  const button = event.target.closest && event.target.closest("#qr-btn-avanza[data-qr-chiedi], #qr-btn-no")
  const form = button && button.form
  const scelte = form && form.querySelector("[data-qr-scelte]")
  const conferma = form && form.querySelector("[data-qr-conferma]")
  if (!scelte || !conferma) return
  event.preventDefault()
  const chiedi = button.id === "qr-btn-avanza"
  const frase = form.querySelector("#qr-action-confirm")
  if (frase) frase.hidden = !chiedi
  conferma.hidden = !chiedi
  scelte.hidden = chiedi
  const fuoco = form.querySelector(chiedi ? "#qr-btn-si" : "#qr-btn-avanza")
  if (fuoco) fuoco.focus()
})
// Un invio solo del modulo del pannello (review della 054, M1): un doppio tocco su «✅ Pronta» o su
// «Sì» manda il modulo una volta. Il pulsante premuto non si spegne: un pulsante spento non manda il
// suo valore, e conferma=1 si perderebbe. Tornando indietro alla pagina tenuta dal browser il modulo
// riparte.
document.addEventListener("submit", event => {
  const form = event.target.closest && event.target.closest("#qr-action-form")
  if (!form) return
  if (form.dataset.qrInviato) { event.preventDefault(); return }
  form.dataset.qrInviato = "1"
})
window.addEventListener("pageshow", event => {
  if (!event.persisted) return
  document.querySelectorAll("#qr-action-form").forEach(form => { delete form.dataset.qrInviato })
})

// Un invio solo per i moduli segnati data-invio-unico (la nuova busta, la modifica), come phx-submit di
// LiveView, che disabilita i pulsanti e mette i campi in sola lettura finché il server risponde. Il
// server comunque crea una busta sola per lo stesso modulo (campo invio). Tornando indietro alla
// pagina tenuta in memoria dal browser il pulsante torna attivo.
document.addEventListener("submit", event => {
  const form = event.target.closest && event.target.closest("form[data-invio-unico]")
  if (!form) return
  if (form.dataset.inviato) { event.preventDefault(); return }
  form.dataset.inviato = "1"
  form.querySelectorAll("button[type=submit]").forEach(button => { button.disabled = true })
})
window.addEventListener("pageshow", event => {
  if (!event.persisted) return
  document.querySelectorAll("form[data-invio-unico]").forEach(form => {
    delete form.dataset.inviato
    form.querySelectorAll("button[type=submit]").forEach(button => { button.disabled = false })
  })
})

// «📲 Condividi / Stampa» di un PDF dal telefono (RIP-S-44, slice 046): pdf_share.js dello standalone
// (slice 199). Il controllo è un link semplice al download (href con download=1) segnato
// data-share-pdf con l'indirizzo del PDF: qui il tocco scarica il PDF e lo passa come file al menu di
// condivisione del sistema (da lì Android e iOS stampano, o lo mandano su WhatsApp, email, Drive). Il
// nome del file è quello che dà il server in content-disposition. Dove il browser non può condividere
// un file (un PC, un browser vecchio) o qualcosa va storto si scarica lo stesso PDF; una condivisione
// annullata (AbortError) non fa niente. Senza JavaScript il link scarica.
function nomePDF(risposta) {
  const trovato = /filename="([^"]+)"/.exec(risposta.headers.get("content-disposition") || "")
  return trovato ? trovato[1] : "documento.pdf"
}
function scaricaPDF(indirizzo) {
  const link = document.createElement("a")
  link.href = indirizzo + (indirizzo.includes("?") ? "&" : "?") + "download=1"
  link.download = ""
  document.body.appendChild(link)
  link.click()
  link.remove()
}
async function condividiPDF(indirizzo) {
  if (!navigator.share || !navigator.canShare) return scaricaPDF(indirizzo)
  try {
    const risposta = await fetch(indirizzo, {credentials: "same-origin"})
    if (!risposta.ok) throw new Error("PDF non disponibile")
    const file = new File([await risposta.blob()], nomePDF(risposta), {type: "application/pdf"})
    if (!navigator.canShare({files: [file]})) return scaricaPDF(indirizzo)
    await navigator.share({files: [file], title: file.name})
  } catch (errore) {
    if (errore && errore.name === "AbortError") return
    scaricaPDF(indirizzo)
  }
}
document.addEventListener("click", event => {
  const controllo = event.target instanceof Element && event.target.closest("[data-share-pdf]")
  if (!controllo) return
  event.preventDefault()
  condividiPDF(controllo.dataset.sharePdf)
})

// Il visualizzatore delle foto (slice 038), photo_viewer.js dello standalone. Solo comodità: aprire,
// scorrere e chiudere sono ancore e :target, e senza questo codice il visualizzatore funziona lo
// stesso (si perdono scorrimento, frecce, Esc e la voce della cronologia sostituita). I comandi della
// barra sostituiscono la voce della cronologia invece di aggiungerne una; lo scorrimento è quello
// della sorgente: almeno 50 px in orizzontale e più orizzontale che verticale.
;(function(){
  const SWIPE_PX = 50
  const openFrame = () => document.querySelector(".gs-viewer-frame:target")
  const tap = (frame, direction) => {
    const link = frame.querySelector(`a[data-gs-nav="${direction}"]`)
    if (link) { link.click() }
    return !!link
  }
  document.addEventListener("click", event => {
    const target = event.target
    const link = target instanceof Element ? target.closest("a[data-gs-nav]") : null
    if (!link) return
    event.preventDefault()
    window.location.replace(link.getAttribute("href"))
  })
  document.addEventListener("keydown", event => {
    const frame = openFrame()
    if (!frame || event.metaKey || event.ctrlKey || event.altKey) return
    if (event.key === "ArrowLeft" && tap(frame, "prev")) { event.preventDefault() }
    else if (event.key === "ArrowRight" && tap(frame, "next")) { event.preventDefault() }
    else if (event.key === "Escape" && tap(frame, "close")) { event.preventDefault() }
  })
  let startX = 0, startY = 0
  document.addEventListener("touchstart", event => {
    startX = event.touches[0].clientX
    startY = event.touches[0].clientY
  }, {passive: true})
  document.addEventListener("touchend", event => {
    const frame = openFrame()
    if (!frame) return
    const dx = event.changedTouches[0].clientX - startX
    const dy = event.changedTouches[0].clientY - startY
    if (Math.abs(dx) < SWIPE_PX || Math.abs(dy) > Math.abs(dx)) return
    tap(frame, dx > 0 ? "prev" : "next")
  }, {passive: true})
})()

// Le pagine cambiate senza ricaricare (slice 040b): chiedere la pagina al server e metterne il body al
// posto di questo; la striscia #photo-queue resta la stessa. Lo usa il caricamento dalla scheda
// (sotto), che mostra così la pagina che il server dà dopo il redirect senza chiederla due volte (il
// segno della scheda è monouso). Dalla slice 044b la coda delle foto di «Nuova riparazione» vive in
// IndexedDB e riprende da ogni pagina; solo le foto che restano in memoria (senza IndexedDB) vogliono
// ancora il giro fra la lista e «Nuova riparazione» senza ricaricare (la coda, sotto).
// Dopo ogni pagina, anche la prima, parte l'evento gs:pagina. Si mostrano così solo le pagine del
// personale (con la striscia #photo-queue): il login o una risposta d'errore si aprono con una
// navigazione normale, come senza JavaScript.
const paginaPronta = () => document.dispatchEvent(new Event("gs:pagina"))
let paginaCambiata = false
// Percorso e query della pagina mostrata: un salto d'ancora (il visualizzatore delle foto) cambia solo
// l'hash e non è un'altra pagina.
let paginaMostrata = location.pathname + location.search

const leggiPagina = html => new DOMParser().parseFromString(html, "text/html")

// mostraPagina mette al posto di questa la pagina doc, all'indirizzo dato; cronologia: "aggiungi"
// (un link), "sostituisci" (un invio, come il redirect dopo un POST) o niente (avanti e indietro). Il
// fuoco torna sull'elemento con lo stesso id, se la pagina nuova lo ha (come un elemento che LiveView
// ridisegna senza toglierlo).
function mostraPagina(doc, indirizzo, cronologia, tieniScorrimento) {
  const fuoco = document.activeElement && document.activeElement.id
  const coda = document.getElementById("photo-queue")
  const codaNuova = doc.getElementById("photo-queue")
  if (coda && codaNuova) codaNuova.replaceWith(coda)
  document.title = doc.title
  const meta = document.querySelector("meta[name='csrf-token']")
  const metaNuovo = doc.querySelector("meta[name='csrf-token']")
  if (meta && metaNuovo) meta.setAttribute("content", metaNuovo.getAttribute("content"))
  const x = window.scrollX, y = window.scrollY
  document.body.replaceWith(document.adoptNode(doc.body))
  if (cronologia === "aggiungi") history.pushState({gestrip: true}, "", indirizzo)
  else if (cronologia === "sostituisci") history.replaceState({gestrip: true}, "", indirizzo)
  paginaCambiata = true
  paginaMostrata = location.pathname + location.search
  if (tieniScorrimento) window.scrollTo(x, y); else window.scrollTo(0, 0)
  paginaPronta()
  const el = fuoco && document.getElementById(fuoco)
  if (el && typeof el.focus === "function") el.focus({preventScroll: true})
}

// connessioneAssente è il messaggio di LiveView quando la connessione cade (client-error, «We can't
// find the internet» tradotto dallo standalone), per un invio che non arriva al server.
function connessioneAssente() {
  document.querySelectorAll(".flash-connessione").forEach(flash => flash.remove())
  const flash = document.createElement("div")
  flash.setAttribute("role", "alert")
  flash.className = "flash flash-errore flash-connessione"
  flash.textContent = "Connessione assente"
  const main = document.querySelector("main")
  if (main) main.before(flash); else document.body.prepend(flash)
}

// ---- Il QR: suono, lettore con la fotocamera, pannello sopra la lista (slice 055) ----------------

// Il suono e la vibrazione dell'esito del QR (RIP-H-27): qr_sound.js dello standalone (slice 185).
// L'esito è quello del server, mai la sola lettura della fotocamera: un codice letto ma sconosciuto è
// un fallimento. Successo: un bip acuto e breve; fallimento (codice di nessuna busta, azione
// rifiutata): due bip gravi. Oscillatori Web Audio, nessun file audio; la vibrazione dove il browser
// la ha (Android; l'iPhone no). L'audio lo sblocca il tocco su 📷 (iOS e Chrome avviano un
// AudioContext solo dentro un gesto): fino ad allora non suona niente, così un codice scritto a mano
// da un PC resta muto. L'interruttore 🔊/🔇 (#qr-sound-toggle) è la preferenza del dispositivo
// (gs-qr-sound); ogni accesso allo storage è protetto, e uno storage che non funziona lascia il
// suono acceso.
const suonoQR = (() => {
  // [hz, inizio (s), durata (s)]
  const SUCCESSO = [[1760, 0.0, 0.12]]
  const FALLIMENTO = [[330, 0.0, 0.18], [330, 0.28, 0.18]]
  const CHIAVE = "gs-qr-sound"
  let contesto = null

  function acceso() {
    try { return localStorage.getItem(CHIAVE) !== "off" } catch (_e) { return true }
  }
  function scegli(si) {
    try { localStorage.setItem(CHIAVE, si ? "on" : "off") } catch (_e) { /* non tenuto: resta acceso */ }
  }
  // Dentro il tocco su 📷: creare o riprendere qui il contesto è quello che lo fa sentire dopo. Su
  // iPhone l'interruttore del silenzioso spegne Web Audio, se la sessione audio della pagina non è
  // «playback» (Safari 16.4+, dove navigator.audioSession c'è).
  function sblocca() {
    try {
      if (navigator.audioSession) navigator.audioSession.type = "playback"
    } catch (_e) { /* un Safari più vecchio: il silenzioso può spegnerlo */ }
    const Contesto = window.AudioContext || window.webkitAudioContext
    if (!Contesto) return
    try {
      if (!contesto) contesto = new Contesto()
      if (contesto.state === "suspended") contesto.resume().catch(() => {})
    } catch (_e) {
      contesto = null
    }
  }
  function tono(hz, inizio, durata) {
    const oscillatore = contesto.createOscillator()
    const volume = contesto.createGain()
    oscillatore.type = "sine"
    oscillatore.frequency.setValueAtTime(hz, inizio)
    volume.gain.setValueAtTime(0.0001, inizio)
    volume.gain.exponentialRampToValueAtTime(0.35, inizio + 0.01)
    volume.gain.exponentialRampToValueAtTime(0.0001, inizio + durata)
    oscillatore.connect(volume)
    volume.connect(contesto.destination)
    oscillatore.start(inizio)
    oscillatore.stop(inizio + durata + 0.02)
  }
  function suona(ok) {
    if (!contesto || !acceso()) return
    try {
      // iOS lo sospende o lo interrompe quando il telefono si blocca: riprendere, poi i toni.
      if (contesto.state !== "running") contesto.resume().catch(() => {})
      const ora = contesto.currentTime
      for (const [hz, inizio, durata] of ok ? SUCCESSO : FALLIMENTO) tono(hz, ora + inizio, durata)
    } catch (_e) { /* niente suono non è mai un errore */ }
    try {
      if (navigator.vibrate) navigator.vibrate(ok ? 60 : [120, 80, 120])
    } catch (_e) { /* niente vibrazione */ }
  }
  // L'interruttore lo disegna solo questo codice: senza JavaScript resta nascosto.
  function disegna() {
    const el = document.getElementById("qr-sound-toggle")
    if (!el) return
    const si = acceso()
    el.hidden = false
    el.textContent = si ? "🔊" : "🔇"
    el.setAttribute("aria-pressed", si ? "true" : "false")
    el.title = si ? "Suoni del QR accesi: tocca per spegnerli" : "Suoni del QR spenti: tocca per accenderli"
  }
  document.addEventListener("click", event => {
    if (!(event.target.closest && event.target.closest("#qr-sound-toggle"))) return
    scegli(!acceso())
    disegna()
    if (acceso()) {
      sblocca()
      suona(true)
    }
  })
  document.addEventListener("gs:pagina", disegna)
  return {sblocca, suona, disegna}
})()

// Il lettore QR con la fotocamera (RIP-H-28): qr_scanner.js dello standalone, nella lettura singola.
// I fotogrammi vanno su un canvas e li decodifica jsQR (jsqr.min.js, nel binario: guscio.go), caricato
// al primo tocco su 📷. La finestra ha il video con la linea di mira, una riga di stato,
// «🔄 Cambia fotocamera» e «Chiudi». Il codice letto va nel campo di data-target; con data-submit quel
// modulo parte, con il pulsante stesso come mittente (sulla home il 📷 del campo unico, via=qr: la
// ricerca è una scansione), altrimenti (l'etichetta di «Nuova riparazione» e della modifica) riempie
// solo il campo e suona da sé il bip di successo, perché nessun server risponde. Il tocco su 📷
// sblocca l'audio. Senza JavaScript il 📷 della home manda il modulo con via=qr e quello
// dell'etichetta non fa niente.
//
// D-52: le etichette già stampate dallo standalone hanno il QR trasposto (a specchio sulla
// diagonale, misurato nella slice 043). jsQR 1.4.0 le legge già: quando la griglia letta non si
// decodifica la riprova scambiando righe e colonne (decode in jsqr.min.js). Lo prova il banco con
// la fotocamera finta (scenario browser lettore-qr: un QR trasposto e l'etichetta 5 di Chromium).
//
// Sui telefoni con più fotocamere posteriori facingMode "environment" può aprire il teleobiettivo
// (zoom forte, a fuoco solo da lontano): si preferisce la principale e la scelta fatta a mano si
// ricorda (gs-camera-qr, ogni accesso allo storage protetto). Serve un contesto sicuro (HTTPS o
// localhost): senza, il browser non dà la fotocamera e la finestra lo dice.
const PERCORSO_JSQR = "__JSQR__"
const lettoreQR = (() => {
  const CHIAVE_CAMERA = "gs-camera-qr"
  const RISOLUZIONE = {width: {ideal: 1280}, height: {ideal: 720}}
  const OPZIONI = {inversionAttempts: "dontInvert"}
  let jsQRCaricato = null

  function caricaJsQR() {
    if (window.jsQR) return Promise.resolve(window.jsQR)
    if (!jsQRCaricato) {
      jsQRCaricato = new Promise((ok, ko) => {
        const s = document.createElement("script")
        s.src = PERCORSO_JSQR
        s.onload = () => (window.jsQR ? ok(window.jsQR) : ko(new Error("lettore QR non caricato")))
        s.onerror = () => { jsQRCaricato = null; ko(new Error("lettore QR non caricato")) }
        document.head.appendChild(s)
      })
    }
    return jsQRCaricato
  }

  function cameraSalvata() {
    try { return localStorage.getItem(CHIAVE_CAMERA) } catch (_e) { return null }
  }
  function salvaCamera(id) {
    try { localStorage.setItem(CHIAVE_CAMERA, id) } catch (_e) { /* la scelta semplicemente non resta */ }
  }
  function apriCamera(id) {
    const video = Object.assign({}, RISOLUZIONE, id ? {deviceId: {exact: id}} : {facingMode: "environment"})
    return navigator.mediaDevices.getUserMedia({video})
  }
  function cameraDi(flusso) {
    const traccia = flusso && flusso.getVideoTracks()[0]
    return traccia && traccia.getSettings ? traccia.getSettings().deviceId : null
  }
  function ferma(flusso) {
    flusso.getTracks().forEach(traccia => traccia.stop())
  }
  // I nomi delle fotocamere sono vuoti finché una non è stata concessa: si guarda dopo la prima
  // apertura. Su Android «camera2 0» è la principale; un nome senza numero va in fondo.
  async function camerePosteriori() {
    const camere = (await navigator.mediaDevices.enumerateDevices())
      .filter(device => device.kind === "videoinput")
    const dietro = camere.filter(device => /back|rear|environment|posterior/i.test(device.label))
    const indice = device => {
      const match = /camera2? (\d+)/i.exec(device.label)
      return match ? +match[1] : 999
    }
    return (dietro.length ? dietro : camere).sort((a, b) => indice(a) - indice(b))
  }
  // viva() diventa falso se nel frattempo il lettore si chiude: l'apertura si ferma lì e chi chiama
  // spegne quello che ha avuto.
  async function cameraPosteriore(viva) {
    const salvata = cameraSalvata()
    if (salvata) {
      try { return await apriCamera(salvata) } catch (_e) { /* non c'è più: si sceglie di nuovo */ }
    }
    const flusso = await apriCamera(null)
    if (!viva()) return flusso
    let principale = null
    try {
      principale = (await camerePosteriori())[0]
    } catch (_e) { /* resta la fotocamera data da facingMode */ }
    if (viva() && principale && principale.deviceId && principale.deviceId !== cameraDi(flusso)) {
      // Spenta questa, o si apre la principale o di nuovo quella di facingMode; se anche questa non
      // si apre l'errore arriva a chi chiama (mai un flusso già spento da mostrare).
      ferma(flusso)
      try { return await apriCamera(principale.deviceId) } catch (_e) { return await apriCamera(null) }
    }
    return flusso
  }
  // La prossima fotocamera posteriore, ricordata; null se ce n'è una sola.
  async function prossimaCamera(attuale) {
    const camere = await camerePosteriori()
    if (camere.length < 2) return null
    const i = camere.findIndex(device => device.deviceId === cameraDi(attuale))
    const prossima = camere[(i + 1) % camere.length]
    ferma(attuale)
    salvaCamera(prossima.deviceId)
    return apriCamera(prossima.deviceId)
  }

  // leggi è il testo del QR del fotogramma, null se non c'è. D-52: jsQR 1.4.0 legge da sé anche il
  // QR a specchio sulla diagonale (decode di jsqr.min.js: se la griglia letta non si decodifica, la
  // riprova trasposta), quindi un fotogramma basta una volta sola.
  function leggi(jsQR, img) {
    const codice = jsQR(img.data, img.width, img.height, OPZIONI)
    return codice ? codice.data : null
  }

  // Lo stato del lettore: uno solo per pagina. La finestra sta nel body e si rifà se una pagina
  // cambiata senza ricaricare (mostraPagina) ha tolto il body di prima.
  const s = {pulsante: null, finestra: null, video: null, canvas: null, stato: null, flusso: null, apertura: null, raf: null, jsQR: null, erroreJsQR: null, ripiego: null, mira: null}

  function finestra() {
    if (s.finestra && document.body.contains(s.finestra)) return s.finestra
    const el = document.createElement("div")
    el.id = "qr-scanner-modal"
    el.className = "gs-lettore"
    el.hidden = true
    el.setAttribute("role", "dialog")
    el.setAttribute("aria-label", "Scanner QR")
    const quadro = document.createElement("div")
    quadro.className = "gs-lettore-quadro"
    s.video = document.createElement("video")
    s.video.setAttribute("playsinline", "")
    s.video.muted = true
    // La linea di mira (D-78) si vede solo sopra un video: senza fotocamera resterebbe sospesa sul
    // vuoto. mostra la accende, chiudiLettore la spegne.
    const mira = s.mira = document.createElement("div")
    mira.className = "gs-lettore-mira"
    mira.setAttribute("aria-hidden", "true")
    mira.hidden = true
    quadro.append(s.video, mira)
    s.canvas = document.createElement("canvas")
    s.canvas.hidden = true
    s.stato = document.createElement("p")
    s.stato.id = "qr-scanner-status"
    s.stato.className = "gs-lettore-stato"
    s.stato.setAttribute("role", "status")
    const cambia = document.createElement("button")
    cambia.type = "button"
    cambia.id = "qr-scanner-switch"
    cambia.className = "gs-lettore-cambia"
    cambia.textContent = "🔄 Cambia fotocamera"
    cambia.addEventListener("click", cambiaCamera)
    const chiudi = document.createElement("button")
    chiudi.type = "button"
    chiudi.id = "qr-scanner-close"
    chiudi.className = "gs-lettore-chiudi"
    chiudi.textContent = "Chiudi"
    chiudi.addEventListener("click", chiudiDaUtente)
    el.append(quadro, s.canvas, s.stato, cambia, chiudi)
    document.body.appendChild(el)
    s.finestra = el
    return el
  }

  // senzaFotocamera scrive il messaggio di una fotocamera che non si apre (come il lettore della home)
  // e, per il 📷 che riempie solo un campo (l'etichetta di «Nuova riparazione» e della modifica, senza
  // data-submit), segna il campo per il lettore a pistola: quando la finestra si chiude il cursore
  // torna lì (slice 059). Il 📷 della home non ha ripiego: il campo unico è già quello della pistola.
  function senzaFotocamera(messaggio) {
    s.stato.textContent = messaggio
    const p = s.pulsante
    s.ripiego = p && !p.hasAttribute("data-submit") ? document.getElementById(p.getAttribute("data-target") || "") : null
  }

  function apri(pulsante) {
    chiudiLettore()
    s.pulsante = pulsante
    finestra().hidden = false
    s.stato.textContent = "Avvio fotocamera…"
    if (!window.isSecureContext) {
      // Una pagina che non è un contesto sicuro (HTTP, o un certificato autofirmato o non valido) non
      // dà mai la fotocamera: navigator.mediaDevices lì non c'è. È il contesto, non un guasto del
      // programma.
      senzaFotocamera(
        "⚠ Fotocamera non disponibile: contesto non sicuro (HTTP o certificato non valido), il browser non espone la fotocamera.")
      return
    }
    if (!navigator.mediaDevices || !navigator.mediaDevices.getUserMedia) {
      senzaFotocamera("⚠ Fotocamera non disponibile su questo dispositivo o browser.")
      return
    }
    // Una chiusura mentre la fotocamera si apre chiude questa apertura: nessuna altra fotocamera si
    // apre e il flusso arrivato si spegne.
    const apertura = s.apertura = {}
    const viva = () => s.apertura === apertura
    s.erroreJsQR = null
    caricaJsQR().then(jsQR => { if (viva()) s.jsQR = jsQR }, err => {
      if (!viva()) return
      // Senza jsQR la fotocamera non legge niente: il messaggio resta anche quando il video parte.
      s.erroreJsQR = "⚠ " + err.message
      s.stato.textContent = s.erroreJsQR
    })
    cameraPosteriore(viva)
      .then(flusso => {
        if (!viva()) return ferma(flusso)
        mostra(flusso)
        fotogramma()
      })
      .catch(err => {
        // Nessuna fotocamera (NotFoundError), permesso negato (NotAllowedError) o fotocamera occupata.
        if (viva()) senzaFotocamera("⚠ " + err.message)
      })
  }

  function cambiaCamera() {
    if (!s.flusso) return
    const prima = s.flusso
    prossimaCamera(prima)
      .then(flusso => {
        // chiuso (o cambiato di nuovo) nel frattempo: niente da mostrare né da dire
        if (s.flusso !== prima) return flusso && ferma(flusso)
        if (!flusso) {
          s.stato.textContent = "Una sola fotocamera disponibile."
          return
        }
        mostra(flusso)
      })
      .catch(err => {
        if (s.flusso !== prima) return
        // L'elenco può fallire prima che la fotocamera vecchia sia spenta: qui si spegne.
        ferma(prima)
        s.flusso = null
        s.mira.hidden = true
        s.stato.textContent = "⚠ " + err.message
      })
  }

  function mostra(flusso) {
    s.flusso = flusso
    s.mira.hidden = false
    s.video.srcObject = flusso
    s.video.play().catch(() => {})
    s.stato.textContent = s.erroreJsQR || "Inquadra il codice QR…"
  }

  function fotogramma() {
    const video = s.video, canvas = s.canvas
    if (s.jsQR && video.readyState === video.HAVE_ENOUGH_DATA && video.videoWidth > 0) {
      canvas.width = video.videoWidth
      canvas.height = video.videoHeight
      const ctx = canvas.getContext("2d", {willReadFrequently: true})
      ctx.drawImage(video, 0, 0, canvas.width, canvas.height)
      const testo = leggi(s.jsQR, ctx.getImageData(0, 0, canvas.width, canvas.height))
      if (testo !== null) {
        const pulsante = s.pulsante
        chiudiLettore()
        consegna(pulsante, testo)
        return
      }
    }
    s.raf = requestAnimationFrame(fotogramma)
  }

  function consegna(pulsante, testo) {
    const campo = pulsante && document.getElementById(pulsante.getAttribute("data-target") || "")
    if (!campo) return
    campo.value = testo
    campo.dispatchEvent(new Event("input", {bubbles: true}))
    const modulo = pulsante.hasAttribute("data-submit") ? document.getElementById(pulsante.getAttribute("data-submit")) : null
    if (modulo) {
      // Il pulsante come mittente: sulla home manda via=qr, come senza JavaScript.
      if (pulsante.form === modulo) modulo.requestSubmit(pulsante)
      else modulo.requestSubmit()
    } else if (!pulsante.hasAttribute("data-submit")) {
      suonoQR.suona(true)
    }
  }

  function chiudiLettore() {
    s.apertura = null
    s.ripiego = null
    if (s.flusso) {
      ferma(s.flusso)
      s.flusso = null
    }
    if (s.raf) {
      cancelAnimationFrame(s.raf)
      s.raf = null
    }
    if (s.video) s.video.srcObject = null
    if (s.mira) s.mira.hidden = true
    if (s.finestra) s.finestra.hidden = true
  }

  // chiudiDaUtente è «Chiudi» o Esc: dopo una fotocamera che non si è aperta il cursore va nel campo
  // del ripiego (senzaFotocamera), pronto per il lettore a pistola.
  function chiudiDaUtente() {
    const campo = s.ripiego
    chiudiLettore()
    if (campo && document.body.contains(campo)) campo.focus()
  }

  const aperto = () => !!(s.finestra && !s.finestra.hidden && document.body.contains(s.finestra))

  // Il tocco su un 📷 (#qr-lookup-scan del campo unico, #repair-qr-code-scan dell'etichetta) apre il lettore e sblocca l'audio. Il blur: l'Invio di
  // una tastiera o di un lettore a pistola lo toccherebbe di nuovo sotto la finestra.
  document.addEventListener("click", event => {
    const pulsante = event.target.closest && event.target.closest("#qr-lookup-scan, #repair-qr-code-scan")
    if (!pulsante) return
    event.preventDefault()
    pulsante.blur()
    suonoQR.sblocca()
    apri(pulsante)
  })
  // Una pagina cambiata senza ricaricare spegne il lettore rimasto aperto.
  document.addEventListener("gs:pagina", () => { if (s.flusso || s.apertura) chiudiLettore() })
  window.addEventListener("pagehide", chiudiLettore)
  return {aperto, chiudi: chiudiDaUtente}
})()

// Il pannello del QR sopra la lista (D-70, il resto della 054, Dubbio 9): con JavaScript, sulla lista
// (la home, o la lista disegnata dal POST di una ricerca), il modulo del campo unico (🔍, Invio o
// lettore a pistola, e il 📷 dopo la lettura) e i moduli del pannello (azioni, «Sì», le gemelle)
// partono con fetch, come POST con il CSRF; il codice resta nel corpo, mai nell'indirizzo, nemmeno
// con la cronologia. La risposta decide:
//   - la pagina della scansione (#qr-lookup, cercaqr.html): il suo riquadro va sopra la lista, senza
//     il campo unico (la lista ha il suo), con i suoi messaggi; la lista e i filtri restano. Il suono
//     è l'esito che il server ha scritto in data-qr-esito;
//   - un redirect (la scheda di una ricerca con un risultato, la lista o la scheda dopo un'azione
//     riuscita): la pagina del redirect prende il posto di questa (mostraPagina), con il suo indirizzo
//     nella cronologia, come senza JavaScript; dopo un'azione del pannello è il bip di successo (il
//     server fa il redirect solo quando l'azione è riuscita);
//   - la lista disegnata nella risposta (una ricerca scritta con più risultati o nessuno): prende il
//     posto di questa, senza cambiare indirizzo (il testo può essere un codice letto dalla pistola);
//   - il login (sessione finita): la pagina si ricarica e il server porta al login; una risposta che
//     non è una pagina del personale (il 403 del CSRF) si mostra com'è.
// «Chiudi» (e Esc) toglie il riquadro e la lista è ancora lì. Il doppio invio del modulo del pannello lo
// ferma l'ascoltatore della 054 (sopra), il secondo invio della ricerca si ignora finché la prima
// risposta non arriva. Senza JavaScript tutto resta il POST /cerca-qr della 052 e 054.
;(() => {
  // La lista o la galleria della home (slice 057): il pannello va sopra tutte e due.
  const nellaLista = () => document.querySelector("#repairs, #repair-gallery") !== null
  let ricercaInCorso = false

  function mettiPannello(doc, nuovo) {
    nuovo.querySelectorAll("#repair-search-form, #repair-search-token").forEach(el => el.remove())
    document.querySelectorAll("body > .flash").forEach(el => el.remove())
    const main = document.querySelector("main")
    doc.querySelectorAll("body > .flash").forEach(el => main.before(document.adoptNode(el)))
    const meta = document.querySelector("meta[name='csrf-token']")
    const metaNuovo = doc.querySelector("meta[name='csrf-token']")
    if (meta && metaNuovo) meta.setAttribute("content", metaNuovo.getAttribute("content"))
    const el = document.adoptNode(nuovo)
    const vecchio = document.getElementById("qr-lookup")
    if (vecchio) vecchio.replaceWith(el)
    else {
      const dopo = document.getElementById("repair-search-token") || document.getElementById("repair-search-form")
      if (dopo) dopo.after(el); else main.prepend(el)
    }
    suonoQR.disegna()
    const fuoco = el.querySelector("#qr-btn-avanza, #qr-btn-si, [id^='qr-match-'], #qr-btn-consulta, #qr-lookup-close")
    if (fuoco) fuoco.focus({preventScroll: true})
    el.scrollIntoView({block: "nearest"})
  }

  function risposta(r, html, dalPannello) {
    const doc = leggiPagina(html)
    const delPersonale = doc.getElementById("photo-queue") !== null
    if (new URL(r.url).pathname === "/login") { window.location.reload(); return }
    if (!delPersonale) {
      document.open()
      document.write(html)
      document.close()
      return
    }
    const nuovo = doc.getElementById("qr-lookup")
    if (nuovo) {
      const esito = nuovo.getAttribute("data-qr-esito")
      mettiPannello(doc, nuovo)
      if (esito === "ok" || esito === "ko") suonoQR.suona(esito === "ok")
      return
    }
    if (dalPannello && r.redirected) suonoQR.suona(true)
    // La lista di una ricerca scritta: un'etichetta che non è di nessuna busta (il lettore a pistola)
    // porta l'esito ko del server (#repair-search-outcome, slice 059), i due bip gravi; ogni altra
    // ricerca scritta resta muta.
    const esitoLista = doc.getElementById("repair-search-outcome")
    const koLista = !dalPannello && esitoLista !== null && esitoLista.getAttribute("data-qr-esito") === "ko"
    mostraPagina(doc, r.url, r.redirected ? "aggiungi" : null)
    if (koLista) suonoQR.suona(false)
  }

  document.addEventListener("submit", event => {
    const form = event.target
    if (event.defaultPrevented || !(form instanceof HTMLFormElement) || !nellaLista()) return
    const ricerca = form.id === "repair-search-form"
    const dalPannello = !ricerca && form.closest("#qr-lookup") !== null
    if (!ricerca && !dalPannello) return
    event.preventDefault()
    if (ricerca && ricercaInCorso) return
    const mittente = event.submitter
    const azione = mittente && mittente.hasAttribute("formaction") ? mittente.formAction : form.action
    const corpo = new URLSearchParams(new FormData(form))
    if (mittente && mittente.name) corpo.append(mittente.name, mittente.value)
    if (ricerca) ricercaInCorso = true
    fetch(azione, {method: "POST", body: corpo, credentials: "same-origin", headers: {accept: "text/html"}})
      .then(r => r.text().then(html => risposta(r, html, dalPannello)))
      .catch(() => {
        delete form.dataset.qrInviato
        connessioneAssente()
      })
      .finally(() => { ricercaInCorso = false })
  })

  function chiudiPannello() {
    const el = document.getElementById("qr-lookup")
    if (!el || !nellaLista()) return false
    el.remove()
    document.querySelectorAll("body > .flash").forEach(f => f.remove())
    const campo = document.getElementById("repair-search-code")
    if (campo) campo.focus({preventScroll: true})
    return true
  }
  document.addEventListener("click", event => {
    if (!(event.target.closest && event.target.closest("#qr-lookup-close")) || !nellaLista()) return
    event.preventDefault()
    chiudiPannello()
  })
  // Esc chiude prima il lettore, se è aperto, poi il riquadro (qr_escape dello standalone).
  document.addEventListener("keydown", event => {
    if (event.key !== "Escape") return
    if (lettoreQR.aperto()) { lettoreQR.chiudi(); return }
    if (document.querySelector("[data-theme-picker]:not([hidden])")) return
    chiudiPannello()
  })
})()

// La coda delle foto scelte prima di generare la busta (RIP-H-40): photo_queue.js dello standalone
// (slice 190), copiata nella slice 040b, e dalla slice 044b in background davvero (D-58, D-59).
// «Nuova riparazione» ha i due pulsanti (al più 4 foto per fonte, D-59); le foto scelte restano nel
// browser, con le anteprime, finché «Genera busta» non ha salvato la busta. La pagina dell'esito porta
// l'avviso #intake-photos-saved con id e numero della busta nuova e la chiave del modulo salvato
// (l'evento intake-photos-saved dello standalone): le foto scelte con quella chiave passano alla
// coda, che le manda una per richiesta a POST /repairs/{id}/photos (XHR, per la barra). Le foto partono
// come sono, mai ridimensionate né ricompresse qui: il server le tiene in JPEG alla risoluzione del
// telefono.
//
// Nello standalone la coda vive in memoria fuori dal contenitore LiveView: sopravvive alla navigazione
// live, si perde ricaricando, e chiede conferma prima di uscire. Qui ogni pagina è un documento, quindi
// la coda vive nel browser, in IndexedDB (base gestrip-coda-foto: le voci nel magazzino «voci», i file
// in «file», così cambiare lo stato di una voce non riscrive la foto):
//   - ogni foto scelta è una voce subito, con un identificativo casuale (22 caratteri base64url),
//     l'utente della pagina (meta gestrip-utente), la scheda del browser (sessionStorage) e la chiave
//     del modulo; un modulo ridisegnato con la stessa chiave (avviso del QR, salvataggio rifiutato) la
//     ritrova, un modulo con un'altra chiave toglie quelle della stessa scheda e lo dice, come lo
//     standalone, ma mai quelle che una pagina ancora aperta tiene nel suo modulo (lo chiede sul
//     canale: una scheda duplicata ha lo stesso sessionStorage);
//   - «Genera busta» annota in sessionStorage la chiave del modulo mandato; l'avviso della busta
//     salvata si annota subito in sessionStorage (scrittura sincrona: un clic immediato su un link non
//     lo perde) e poi, in una transazione, le voci del modulo mandato passano «in attesa» con la busta
//     se la chiave dell'avviso è la loro, altrimenti si tolgono. Rifatto (una scheda duplicata con lo
//     stesso sessionStorage) non trova più voci da cambiare: vale una volta sola;
//   - ogni pagina interna (quella con la striscia #photo-queue) guarda la coda e manda le voci in
//     attesa dell'utente della pagina, una alla volta, nell'ordine delle buste; una voce salita si
//     toglie. Una pagina lasciata a metà invio: la pagina dopo la manda di nuovo, con lo stesso
//     identificativo in x-upload-id, e il server risponde con la foto già salvata, senza doppioni;
//   - con più schede aperte ne manda una sola: il lucchetto gestrip-coda-foto delle Web Locks, o senza
//     un affitto in localStorage rinnovato mentre si manda (se due schede mandassero insieme la
//     stessa foto, il server ne salva comunque una); le altre schede ridisegnano la striscia quando
//     la coda cambia (BroadcastChannel);
//   - un'altra persona sullo stesso browser non manda le foto di chi le ha scelte; con «Esci» le foto
//     restano per quando lo stesso utente rientra; le voci in coda o rifiutate si tolgono da sole dopo
//     QUEUE_MAX_MS (14 giorni), le foto scelte e mai salvate dopo un giorno;
//   - nessuna conferma prima di uscire. Solo se IndexedDB non c'è o non scrive (alcuni Safari in
//     privato, quota piena) la voce resta in memoria come nello standalone: per quelle voci resta la
//     conferma prima di uscire e il giro fra la lista e «Nuova riparazione» senza ricaricare (sotto).
// Errori: 413, 415, 422 sono definitivi, come PERMANENT dello standalone (la foto resta nella
// striscia, rifiutata, finché non la si chiude con ✕); gli altri, 404 compreso, si riprovano. Il
// numero dei tentativi e quando riprovare stanno nella voce, quindi valgono per ogni pagina e scheda:
// nessun invio prima del momento scritto, nemmeno aprendo una pagina nuova; l'attesa cresce (5 s,
// 15 s, 30 s, 1 min) e dopo MAX_TRIES errori di fila la coda si ferma e aspetta «Riprova». La riga
// dell'errore resta finché un invio riesce o si preme «Riprova»; finché i tentativi automatici
// continuano, accanto a «Riprova» c'è «nuovo tentativo tra N s…» (slice 055d). Una foto non riuscita trattiene le
// foto dopo della sua busta (prenderebbero il suo posto di foto della busta), mai quelle di un'altra.
;(function(){
  // MediaStore.max_input_bytes/0 sul server, che sopra risponde 413.
  const MAX_BYTES = 25 * 1024 * 1024
  const PER_SOURCE = 4
  const TYPES = {".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".webp": "image/webp"}
  const MIMES = new Set(Object.values(TYPES))
  const DROPPED = "Le foto scelte prima sono state tolte: scegli di nuovo quelle di questa busta."
  // Quello che il server ha rifiutato per com'è la foto sarebbe rifiutato di nuovo: quella foto si
  // mette da parte e si va avanti con la prossima della busta.
  const PERMANENT = new Set([413, 415, 422])
  // Le attese prima del secondo, terzo, quarto e quinto tentativo; dopo il quinto errore di fila ci si
  // ferma fino a «Riprova».
  const RETRY_MS = [5000, 15000, 30000, 60000]
  const MAX_TRIES = 5
  // Le foto scelte e mai salvate si tolgono dopo un giorno; quelle in coda o rifiutate dopo
  // QUEUE_MAX_MS dalla busta salvata.
  const STALE_MS = 24 * 60 * 60 * 1000
  const QUEUE_MAX_MS = 14 * 24 * 60 * 60 * 1000
  // «Genera busta» aspetta al più tanto le foto scelte che si stanno ancora scrivendo.
  const WRITE_WAIT_MS = 5000
  // Quanto si aspettano le risposte delle altre pagine sul canale.
  const ASK_MS = 250
  const NAME = "gestrip-coda-foto"
  const SAVED = "gestrip-coda-foto-esito"
  const SUBMITTED = "gestrip-coda-foto-inviato"
  const LEASE = "gestrip-coda-foto-lucchetto"

  function newId() {
    const bytes = new Uint8Array(16)
    crypto.getRandomValues(bytes)
    return btoa(String.fromCharCode(...bytes)).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "")
  }

  // La scheda del browser: tiene insieme le foto scelte da un modulo anche se la pagina si ricarica
  // (sessionStorage è della scheda). Una scheda duplicata ha lo stesso valore: per questo una pagina
  // chiede sul canale prima di togliere qualcosa.
  const tab = (() => {
    try {
      let id = sessionStorage.getItem(NAME)
      if (!id) { id = newId(); sessionStorage.setItem(NAME, id) }
      return id
    } catch (_error) { return "" }
  })()
  // Questa pagina, per l'affitto del ripiego senza Web Locks e per il canale.
  const pageId = newId()

  const session = {
    get(key) { try { return JSON.parse(sessionStorage.getItem(key) || "null") } catch (_error) { return null } },
    set(key, value) { try { sessionStorage.setItem(key, JSON.stringify(value)) } catch (_error) { /* senza: vale la memoria */ } },
    drop(key) { try { sessionStorage.removeItem(key) } catch (_error) { /* niente */ } },
  }

  const currentUser = () => {
    const meta = document.querySelector("meta[name='gestrip-utente']")
    const id = meta ? meta.getAttribute("content") : ""
    return id && id !== "0" ? id : ""
  }

  // --- dove stanno le voci: IndexedDB, o la memoria se IndexedDB non c'è o non scrive -------------

  const store = (() => {
    if (typeof indexedDB === "undefined") return Promise.resolve(null)
    return new Promise(resolve => {
      let request
      try { request = indexedDB.open("gestrip-coda-foto", 1) } catch (_error) { return resolve(null) }
      request.onupgradeneeded = () => {
        request.result.createObjectStore("voci", {keyPath: "id"})
        request.result.createObjectStore("file")
      }
      request.onsuccess = () => {
        const db = request.result
        db.onversionchange = () => db.close()
        resolve(db)
      }
      request.onerror = () => resolve(null)
      request.onblocked = () => resolve(null)
    })
  })()

  function transaction(names, mode, work) {
    return store.then(db => {
      if (!db) throw new Error("IndexedDB assente")
      return new Promise((resolve, reject) => {
        const tx = db.transaction(names, mode)
        let result
        const request = work(tx, value => { result = value })
        if (request) request.onsuccess = () => { result = request.result }
        tx.oncomplete = () => resolve(result)
        tx.onerror = tx.onabort = () => reject(tx.error || new Error("IndexedDB"))
      })
    })
  }

  // Le voci tenute in memoria (senza IndexedDB): come la coda dello standalone, si perdono uscendo.
  const volatili = []
  const inAttesa = item => item.state === "attesa"

  // Le voci senza il file (che sta a parte); la memoria le ha con il file. Una voce che c'è nei due
  // posti (una scrittura finita dopo il tempo di «Genera busta») vale una volta, quella in memoria.
  async function allItems() {
    let saved = []
    try { saved = (await transaction(["voci"], "readonly", tx => tx.objectStore("voci").getAll())) || [] } catch (_error) { saved = [] }
    const memory = new Set(volatili.map(item => item.id))
    return saved.filter(item => !memory.has(item.id)).concat(volatili)
  }

  function forget(item) {
    const index = volatili.indexOf(item)
    if (index >= 0) volatili.splice(index, 1)
  }

  function toRecord(item) {
    const record = {...item}
    delete record.file
    delete record.volatile
    return record
  }

  // putItem salva la voce (con il file la prima volta) e dice se è riuscito. Una voce nuova che
  // IndexedDB non scrive resta in memoria; un cambio di stato che non si scrive lascia la voce com'era
  // (overrides, sotto, lo ricorda a questa pagina).
  async function putItem(item, file) {
    if (!item.volatile) {
      try {
        await transaction(["voci", "file"], "readwrite", tx => {
          tx.objectStore("voci").put(toRecord(item))
          if (file) tx.objectStore("file").put(file, item.id)
        })
        return true
      } catch (_error) {
        if (!file) return false
        item.volatile = true
      }
    }
    if (file) item.file = file
    if (!volatili.includes(item)) volatili.push(item)
    return true
  }

  // putItems salva i cambi di stato di più voci in una transazione sola.
  async function putItems(items) {
    const saved = items.filter(item => !item.volatile)
    if (saved.length === 0) return true
    try {
      await transaction(["voci"], "readwrite", tx => { for (const item of saved) tx.objectStore("voci").put(toRecord(item)) })
      return true
    } catch (_error) {
      return false
    }
  }

  // dropItems toglie le voci (e i loro file, o solo i file con onlyFiles) e dice se è riuscito.
  async function dropItems(items, onlyFiles = false) {
    const saved = []
    for (const item of items) {
      if (item.volatile) { if (onlyFiles) delete item.file; else forget(item) } else saved.push(item.id)
    }
    if (saved.length === 0) return true
    try {
      await transaction(["voci", "file"], "readwrite", tx => {
        for (const id of saved) {
          if (!onlyFiles) tx.objectStore("voci").delete(id)
          tx.objectStore("file").delete(id)
        }
      })
      return true
    } catch (_error) {
      return false
    }
  }

  // fileOf dà il file della voce, null se non c'è più; un errore di IndexedDB resta un errore (da
  // riprovare), non una foto sparita.
  async function fileOf(item) {
    if (item.volatile) return item.file || null
    const file = await transaction(["file"], "readonly", tx => tx.objectStore("file").get(item.id))
    return file || null
  }

  // Le foto in coda non si devono perdere se il browser ha poco spazio: si chiede che la base resti,
  // una volta, senza aspettare la risposta.
  let persistAsked = false
  function askPersist() {
    if (persistAsked) return
    persistAsked = true
    try {
      if (navigator.storage && typeof navigator.storage.persist === "function") navigator.storage.persist().catch(() => {})
    } catch (_error) { /* niente */ }
  }

  // --- il canale fra le pagine ----------------------------------------------------------------------

  const channel = typeof BroadcastChannel === "function" ? new BroadcastChannel("gestrip-coda-foto") : null
  const announce = () => { if (channel) { try { channel.postMessage({kind: "changed"}) } catch (_error) { /* niente */ } } }
  const answers = new Map()

  // heldElsewhere dice quali delle chiavi date una pagina ancora aperta tiene nel suo modulo (una scheda
  // duplicata, un'altra scheda con lo stesso sessionStorage). Senza canale: nessuna.
  async function heldElsewhere(keys) {
    if (!channel || keys.length === 0) return new Set()
    const ask = newId()
    const held = new Set()
    answers.set(ask, held)
    try { channel.postMessage({kind: "who", ask, keys, from: pageId}) } catch (_error) { /* niente */ }
    await new Promise(resolve => setTimeout(resolve, ASK_MS))
    answers.delete(ask)
    return held
  }

  // --- le foto scelte nel modulo sullo schermo ------------------------------------------------------

  // Le voci scelte nel modulo sullo schermo, nell'ordine in cui sono state scelte: la prima di una
  // busta senza foto diventa la foto della busta. La chiave è quella del modulo (data-intake-key,
  // nuova per ogni modulo e per ogni busta salvata): le foto vanno solo alla busta salvata con lei.
  let staged = []
  let stagedKey = null
  const previews = new Map()
  // Le scritture e le cancellazioni delle foto scelte non ancora finite: «Genera busta» le aspetta.
  const writes = []
  function track(promise, item = null) {
    const entry = {promise, item}
    writes.push(entry)
    promise.finally(() => { const i = writes.indexOf(entry); if (i >= 0) writes.splice(i, 1) })
    return promise
  }
  // La sezione delle foto della pagina sullo schermo (l'hook IntakePhotos), se c'è, e gli ultimi
  // errori che ha mostrato (la sezione ridisegnata dal server li tiene, come phx-update="ignore").
  let intake = null
  let lastErrors = []

  function extension(name) {
    const dot = (name || "").lastIndexOf(".")
    return dot < 0 ? "" : name.slice(dot).toLowerCase()
  }

  function photoType(file) {
    if (MIMES.has(file.type)) return file.type
    return TYPES[extension(file.name)] || null
  }

  function refusal(file) {
    if (!photoType(file)) return "Formato non supportato."
    if (file.size > MAX_BYTES) return "Foto oltre il limite di 25 MiB."
    if (file.size === 0) return "Foto vuota."
    return null
  }

  function preview(item, file) {
    if (file) previews.set(item.id, URL.createObjectURL(file))
  }

  function clearStaged() {
    for (const item of staged) {
      const url = previews.get(item.id)
      if (url) URL.revokeObjectURL(url)
      previews.delete(item.id)
    }
    staged = []
  }

  function element(tag, attributes = {}, text = null) {
    const node = document.createElement(tag)
    for (const [key, value] of Object.entries(attributes)) node.setAttribute(key, value)
    if (text !== null) node.textContent = text
    return node
  }

  // mounted() dell'hook: un modulo con un'altra chiave è un'altra busta, e quello che aveva scelto un
  // modulo lasciato da questa scheda non sono le sue foto (lo si dice), a meno che il modulo sia
  // ancora aperto in un'altra pagina. Lo stesso modulo ridisegnato dal server (avviso del QR,
  // salvataggio rifiutato) ha la stessa chiave e ritrova le sue foto, come la sezione con
  // phx-update="ignore" dello standalone.
  async function mountIntake(el) {
    const key = el.dataset.intakeKey
    intake = {
      el,
      count: el.querySelector("[data-intake-count]"),
      status: el.querySelector("#intake-photo-status"),
      errors: el.querySelector("#intake-photo-errors"),
    }
    for (const [id, source] of [["intake-camera-input", "camera"], ["intake-gallery-input", "gallery"]]) {
      const input = el.querySelector(`#${id}`)
      input.addEventListener("change", () => pick(input, source))
    }
    if (key === stagedKey) {
      // La stessa pagina cambiata senza ricaricare (il giro della memoria, sotto).
      el.hidden = false
      return drawIntake(lastErrors)
    }
    const user = currentUser()
    const mine = (await allItems()).filter(item => item.state === "scelta" && item.user === user && item.tab === tab)
    const same = mine.filter(item => item.key === key).sort(byOrder)
    const otherKeys = [...new Set(mine.filter(item => item.key !== key).map(item => item.key))]
    const held = await heldElsewhere(otherKeys)
    const others = mine.filter(item => item.key !== key && !held.has(item.key))
    await dropItems(others)
    clearStaged()
    stagedKey = key
    staged = same
    for (const item of same) {
      let file = null
      try { file = await fileOf(item) } catch (_error) { file = null }
      preview(item, file)
    }
    el.hidden = false
    drawIntake(others.length > 0 ? [DROPPED] : [])
  }

  const byOrder = (a, b) => (a.created - b.created) || (a.order - b.order)
  let sequence = 0

  function pick(input, source) {
    const files = Array.from(input.files || [])
    // Svuotato subito, così la stessa foto si può scegliere di nuovo dopo averla tolta.
    input.value = ""
    if (files.length === 0) return
    const held = staged.filter(photo => photo.source === source).length
    if (held + files.length > PER_SOURCE) return drawIntake(["Massimo 4 foto per pulsante."]) // 055d, punto 1 della 049
    const errors = []
    for (const file of files) {
      const problem = refusal(file)
      if (problem) { errors.push(`${file.name}: ${problem}`); continue }
      const item = {id: newId(), user: currentUser(), tab, key: stagedKey, state: "scelta", source,
        name: file.name, type: photoType(file), created: Date.now(), order: sequence++}
      staged.push(item)
      preview(item, file)
      item.pendingFile = file
      track(putItem(item, file).finally(() => { delete item.pendingFile }), item)
    }
    drawIntake(errors)
  }

  function remove(photo) {
    const index = staged.indexOf(photo)
    if (index >= 0) staged.splice(index, 1)
    const url = previews.get(photo.id)
    if (url) URL.revokeObjectURL(url)
    previews.delete(photo.id)
    // Dopo la sua scrittura, se non è finita; «Genera busta» aspetta anche questa.
    const before = writes.map(entry => entry.promise)
    track(Promise.allSettled(before).then(() => dropItems([photo])))
    drawIntake([])
  }

  function drawIntake(errors) {
    if (!intake) return
    lastErrors = errors
    intake.count.value = String(staged.length)
    intake.status.replaceChildren()
    if (staged.length > 0) {
      const ready = staged.length === 1 ? "1 foto pronta" : `${staged.length} foto pronte`
      intake.status.append(element("p", {}, `${ready} per la busta.`))
    }
    for (const photo of staged) {
      const row = element("div", {class: "riga-foto-scelta"})
      row.append(
        element("img", {
          src: previews.get(photo.id) || "",
          alt: `Anteprima di ${photo.name}`,
          class: "anteprima-foto",
        }),
        element("span", {class: "nome-foto"}, photo.name),
      )
      const button = element("button", {
        type: "button",
        class: "pulsante-secondario",
        "aria-label": `Rimuovi ${photo.name}`,
      }, "Rimuovi")
      button.addEventListener("click", () => remove(photo))
      row.append(button)
      intake.status.append(row)
    }
    intake.errors.replaceChildren(...errors.map(message => element("p", {}, message)))
  }

  // --- l'avviso della busta salvata ----------------------------------------------------------------

  // L'avviso (l'evento intake-photos-saved): si annota subito, in modo sincrono, perché un clic
  // immediato su un link non lo perda.
  function noteSaved(marker) {
    const {repairId, bagNumber: bag, intakeKey: key} = marker.dataset
    marker.remove()
    const submitted = session.get(SUBMITTED)
    session.drop(SUBMITTED)
    const saved = {repairId: repairId || "", bag: bag || "", key: key || "", user: currentUser(),
      submitted: submitted ? submitted.key : (key || "")}
    session.set(SAVED, saved)
    return saved
  }

  // applySaved, in una transazione: le voci del modulo mandato passano alla coda se la busta è stata
  // salvata con la chiave sotto cui sono state scelte, altrimenti si tolgono (non sono più di nessun
  // modulo). Rifatto non trova più voci scelte: vale una volta.
  async function applySaved(saved) {
    const matches = item => item.state === "scelta" && item.user === saved.user && item.key === saved.submitted
    const promote = saved.repairId && saved.key === saved.submitted
    const now = Date.now()
    const shape = list => {
      const theirs = list.filter(matches).sort(byOrder)
      theirs.forEach((item, index) => Object.assign(item, {state: "attesa", repairId: saved.repairId, bag: saved.bag,
        job: saved.key, total: theirs.length, position: index, jobCreated: now, tries: 0, retryAt: 0, error: ""}))
      return theirs
    }
    let moved = 0
    try {
      moved = await transaction(["voci", "file"], "readwrite", (tx, done) => {
        const voci = tx.objectStore("voci")
        const request = voci.getAll()
        request.onsuccess = () => {
          const theirs = shape(request.result || [])
          for (const item of theirs) {
            if (promote) voci.put(item)
            else { voci.delete(item.id); tx.objectStore("file").delete(item.id) }
          }
          done(promote ? theirs.length : 0)
        }
      }) || 0
    } catch (_error) {
      moved = 0
    }
    // Le voci in memoria.
    const theirs = shape(volatili)
    if (!promote) theirs.forEach(forget)
    else moved += theirs.length
    clearStaged()
    session.drop(SAVED)
    if (moved > 0) { askPersist(); announce() }
  }

  // Le foto scelte da moduli mai salvati dopo un giorno; le voci in coda o rifiutate dopo 14 giorni.
  const expired = (item, now) => item.state === "scelta"
    ? now - item.created > STALE_MS
    : now - (item.jobCreated || item.created) > QUEUE_MAX_MS
  async function dropStale() {
    const now = Date.now()
    const old = (await allItems()).filter(item => expired(item, now))
    if (old.length > 0) { await dropItems(old); announce() }
  }

  // --- la striscia e l'invio ------------------------------------------------------------------------

  // Le buste dell'utente della pagina che aspettano le loro foto, dall'ultima lettura della coda:
  // {job, repairId, bag, total, pending (voci da mandare, in ordine), rejected (i motivi)}.
  let jobs = []
  // La foto che questa pagina sta mandando: {job, part}.
  let sending = null
  // Gli stati che questa pagina ha deciso e IndexedDB non ha scritto (una foto salita, rifiutata, un
  // tentativo contato): senza, la stessa foto ripartirebbe di continuo.
  const overrides = new Map()
  // La pagina sta per andarsene: un invio interrotto da questo non è un errore della foto.
  let leaving = false
  window.addEventListener("pagehide", () => { leaving = true })
  window.addEventListener("pageshow", () => { leaving = false })

  async function refresh() {
    const user = currentUser()
    const groups = new Map()
    const now = Date.now()
    let stale = false
    for (const stored of await allItems()) {
      const item = overrides.has(stored.id) ? Object.assign(stored, overrides.get(stored.id)) : stored
      // Una voce scaduta non si mostra né si manda, anche se è arrivata dopo l'inizio della pagina.
      if (expired(item, now)) { stale = true; continue }
      if (!user || item.user !== user || (item.state !== "attesa" && item.state !== "rifiutata")) continue
      if (!groups.has(item.job)) {
        groups.set(item.job, {job: item.job, repairId: item.repairId, bag: item.bag, total: item.total, created: item.jobCreated,
          pending: [], rejected: [], items: []})
      }
      const group = groups.get(item.job)
      group.items.push(item)
      if (item.state === "attesa") group.pending.push(item); else group.rejected.push(item.reason)
    }
    jobs = [...groups.values()].sort((a, b) => a.created - b.created)
    for (const job of jobs) job.pending.sort((a, b) => a.position - b.position)
    draw()
    if (stale) dropStale()
  }

  // La foto da mandare della busta, se c'è: la prima che aspetta.
  const head = job => job.pending[0] || null
  // Pronta a partire: nessun errore, o il momento scritto nella voce è passato e non si è fermata.
  // Ferma dopo MAX_TRIES tentativi andati male: aspetta «Riprova».
  const stopped = photo => !!photo.error && (photo.tries || 0) >= MAX_TRIES
  // Pronta a partire: non ferma e il momento scritto nella voce è passato.
  const due = (photo, now) => !stopped(photo) && (photo.retryAt || 0) <= now

  // Il conto alla rovescia del prossimo tentativo automatico: «nuovo tentativo tra N s…», aggiornato
  // ogni secondo finché la riga c'è; a zero, «nuovo tentativo…».
  let ticker = null
  function countdown(span) {
    const left = Math.ceil((Number(span.dataset.retryAt) - Date.now()) / 1000)
    span.textContent = left > 0 ? `nuovo tentativo tra ${left} s…` : "nuovo tentativo…"
  }
  function tick() {
    const spans = document.querySelectorAll("#photo-queue .attesa-coda")
    spans.forEach(countdown)
    if (spans.length === 0) { clearInterval(ticker); ticker = null }
  }

  function draw() {
    const strip = document.getElementById("photo-queue")
    if (!strip) return
    strip.hidden = jobs.length === 0
    strip.replaceChildren(...jobs.map(job => {
      const total = job.total
      const first = head(job)
      const failure = first && first.error ? first : null
      const row = element("div", {
        "data-repair-id": String(job.repairId),
        class: "riga-coda" + (failure ? " riga-coda-errore" : ""),
      })
      const close = () => {
        const button = element("button", {
          type: "button",
          class: "chiudi-coda",
          "aria-label": `Lascia perdere le foto della busta ${job.bag}`,
        }, "✕")
        button.addEventListener("click", () => {
          dropItems(job.items).then(() => { announce(); return refresh() })
        })
        return button
      }
      const done = total - job.pending.length
      const sent = done - job.rejected.length
      if (failure) {
        row.setAttribute("role", "alert")
        row.append(element("span", {class: "testo-coda"},
          `❌ Busta ${job.bag}: ${sent}/${total} foto caricate, ${failure.error}.`))
        const retry = element("button", {type: "button", class: "riprova-coda"}, "Riprova")
        retry.addEventListener("click", () => {
          Object.assign(failure, {error: "", tries: 0, retryAt: 0})
          overrides.delete(failure.id)
          putItem(failure, null).then(() => { announce(); return refresh() }).then(pump)
        })
        row.append(retry)
        // Slice 055d (punto 3 della 049, domanda 3 della 051): «Riprova» resta, e mentre i tentativi
        // automatici continuano accanto c'è quanto manca al prossimo.
        if (!stopped(failure) && (failure.retryAt || 0) > Date.now()) {
          const wait = element("span", {class: "attesa-coda", "data-retry-at": String(failure.retryAt)}, "")
          countdown(wait)
          row.append(wait)
        }
        row.append(close())
      } else if (job.pending.length === 0) {
        // Finita, ma con foto che il server ha rifiutato: lo dice finché non la si chiude.
        row.setAttribute("role", "alert")
        const refused = job.rejected.length === 1 ? "1 rifiutata" : `${job.rejected.length} rifiutate`
        row.append(element("span", {class: "testo-coda"},
          `❌ Busta ${job.bag}: ${sent}/${total} foto caricate, ${refused} (${[...new Set(job.rejected)].join(", ")}).`), close())
      } else {
        const part = sending && sending.job === job.job ? sending.part : 0
        const percent = Math.round(((done + part) / total) * 100)
        row.append(
          element("span", {class: "testo-coda"}, `📷 Busta ${job.bag}: ${sent}/${total} foto, ${percent}%`),
          element("progress", {value: String(percent), max: "100", "aria-label": `Foto della busta ${job.bag}`}),
        )
      }
      return row
    }))
    if (ticker === null && strip.querySelector(".attesa-coda")) ticker = setInterval(tick, 1000)
  }

  function csrfToken() {
    const meta = document.querySelector("meta[name='csrf-token']")
    return meta ? meta.getAttribute("content") : ""
  }

  function reason(xhr) {
    let path = ""
    try { path = new URL(xhr.responseURL).pathname } catch (_error) { path = "" }
    // Una sessione finita è un redirect che l'XHR segue fino al login.
    if (path === "/login" || xhr.status === 401 || xhr.status === 403) return "accesso scaduto, rientra e riprova"
    if (xhr.status === 413) return "foto oltre il limite di 25 MiB"
    if (xhr.status === 415) return "formato non supportato"
    if (xhr.status === 422) return "foto rifiutata"
    if (xhr.status === 404) return "busta non trovata"
    return `errore ${xhr.status}`
  }

  // send manda una foto e dice com'è andata: {status, error}. Un 201 è riuscito anche con photo_id
  // null (la foto di quell'invio è stata tolta dalla scheda: già fatto).
  function send(photo, file) {
    return new Promise(resolve => {
      const xhr = new XMLHttpRequest()
      sending = {job: photo.job, part: 0}
      xhr.open("POST", `/repairs/${encodeURIComponent(photo.repairId)}/photos`)
      xhr.setRequestHeader("x-csrf-token", csrfToken())
      xhr.setRequestHeader("content-type", photo.type)
      xhr.setRequestHeader("x-file-name", encodeURIComponent((photo.name || "").slice(-200)))
      // L'identificativo della foto: un secondo invio (pagina cambiata a metà) non fa un doppione.
      xhr.setRequestHeader("x-upload-id", photo.id)
      // Un caricamento fermo da telefono deve finire, o ogni busta dietro aspetta per sempre.
      xhr.timeout = 300000
      xhr.upload.onprogress = event => {
        if (!event.lengthComputable || !sending) return
        sending.part = event.loaded / event.total
        draw()
      }
      xhr.onload = () => resolve({status: xhr.status, error: xhr.status === 201 ? null : reason(xhr)})
      xhr.onerror = () => resolve({status: 0, error: "connessione assente"})
      xhr.onabort = () => resolve({status: 0, error: "invio interrotto"})
      xhr.ontimeout = () => resolve({status: 0, error: "tempo scaduto"})
      xhr.send(file)
    })
  }

  // reserve conta il tentativo nella voce prima di mandare: tentativi e momento del prossimo sono già
  // scritti anche se la risposta arriva mentre la pagina se ne va e il suo esito non si scrive più.
  // Dà come tornare indietro (un invio interrotto dall'uscita non conta).
  async function reserve(photo) {
    const before = {tries: photo.tries || 0, retryAt: photo.retryAt || 0}
    const tries = before.tries + 1
    const change = {tries, retryAt: Date.now() + RETRY_MS[Math.min(tries - 1, RETRY_MS.length - 1)]}
    Object.assign(photo, change)
    if (!await putItem(photo, null)) overrides.set(photo.id, {...overrides.get(photo.id), ...change})
    return before
  }

  async function finish(photo, outcome, before) {
    sending = null
    // Un invio interrotto perché la pagina se ne va non conta: la pagina dopo lo rifà subito (se il
    // ritorno indietro fa in tempo a scriversi; altrimenti aspetta il momento già scritto). Una
    // risposta arrivata del server invece conta, anche mentre la pagina se ne va.
    if (leaving && outcome.status === 0) {
      if (before) { Object.assign(photo, before); putItem(photo, null) }
      return
    }
    if (!outcome.error) {
      // Se la voce non si toglie, questa pagina non la manda più; un'altra la manderebbe di nuovo con
      // lo stesso identificativo, e il server risponde con la foto già salvata.
      if (!await dropItems([photo])) overrides.set(photo.id, {state: "salita"})
    } else if (PERMANENT.has(outcome.status)) {
      Object.assign(photo, {state: "rifiutata", reason: outcome.error, error: ""})
      if (await putItem(photo, null)) {
        // Il file di una foto rifiutata non serve più: resta la riga nella striscia.
        await dropItems([photo], true)
      } else {
        overrides.set(photo.id, {state: "rifiutata", reason: outcome.error, error: ""})
      }
    } else {
      // Il tentativo è già contato nella voce (reserve): vale per ogni pagina e scheda; qui l'errore.
      const tries = before ? (photo.tries || 1) : (photo.tries || 0) + 1
      const change = {error: outcome.error, tries, retryAt: Date.now() + RETRY_MS[Math.min(tries - 1, RETRY_MS.length - 1)]}
      Object.assign(photo, change)
      if (!await putItem(photo, null)) overrides.set(photo.id, change)
    }
    announce()
  }

  // withLock fa work con il lucchetto della coda: una scheda sola alla volta manda.
  function withLock(work) {
    if (navigator.locks && typeof navigator.locks.request === "function") return navigator.locks.request("gestrip-coda-foto", work)
    return withLease(work)
  }

  // Il ripiego senza Web Locks: un affitto in localStorage di 15 s, rinnovato ogni 5 s mentre si
  // manda, lasciato uscendo dalla pagina. Senza localStorage si manda e basta.
  function takeLease() {
    try {
      const lease = JSON.parse(localStorage.getItem(LEASE) || "null")
      if (lease && lease.page !== pageId && lease.until > Date.now()) return false
      localStorage.setItem(LEASE, JSON.stringify({page: pageId, until: Date.now() + 15000}))
      return true
    } catch (_error) { return true }
  }
  function releaseLease() {
    try {
      const lease = JSON.parse(localStorage.getItem(LEASE) || "null")
      if (lease && lease.page === pageId) localStorage.removeItem(LEASE)
    } catch (_error) { /* niente */ }
  }
  window.addEventListener("pagehide", releaseLease)
  async function withLease(work) {
    while (!takeLease()) await new Promise(resolve => setTimeout(resolve, 2000))
    const renew = setInterval(takeLease, 5000)
    try { return await work() } finally { clearInterval(renew); releaseLease() }
  }

  let pumping = false
  let again = false
  let timer = null

  // Una foto alla volta, le buste nell'ordine in cui sono nate.
  function pump() {
    if (!currentUser() || !document.getElementById("photo-queue")) return
    if (pumping) { again = true; return }
    pumping = true
    again = false
    withLock(async () => {
      for (;;) {
        await refresh()
        const now = Date.now()
        const job = jobs.find(candidate => head(candidate) && due(head(candidate), now))
        if (!job || leaving) return
        const photo = head(job)
        let file
        try {
          file = await fileOf(photo)
        } catch (_error) {
          // IndexedDB non risponde adesso: si riprova come un errore di rete.
          await finish(photo, {status: 0, error: "foto non leggibile adesso"})
          continue
        }
        draw()
        if (!file) {
          await finish(photo, {status: 422, error: "foto non più disponibile"})
          continue
        }
        const before = await reserve(photo)
        await finish(photo, await send(photo, file), before)
      }
    }).catch(() => { /* il prossimo giro riprova */ }).finally(() => {
      pumping = false
      sending = null
      refresh().then(() => {
        if (again) return pump()
        schedule()
      })
    })
  }

  // Il prossimo tentativo da solo di una busta ferma su un errore da riprovare, al momento scritto
  // nella voce; una busta fermata dopo MAX_TRIES errori aspetta «Riprova».
  function schedule() {
    clearTimeout(timer)
    const waits = jobs.map(head).filter(photo => photo && !stopped(photo) && (photo.retryAt || 0) > Date.now()).map(photo => photo.retryAt)
    if (waits.length > 0) timer = setTimeout(pump, Math.max(0, Math.min(...waits) - Date.now()))
  }

  if (channel) {
    channel.onmessage = event => {
      const message = event.data || {}
      if (message.kind === "who") {
        // Un'altra pagina chiede se un modulo è ancora aperto: questo, se ha quella chiave.
        if (message.from !== pageId && stagedKey && message.keys.includes(stagedKey)) {
          try { channel.postMessage({kind: "mine", ask: message.ask, key: stagedKey}) } catch (_error) { /* niente */ }
        }
      } else if (message.kind === "mine") {
        const held = answers.get(message.ask)
        if (held) held.add(message.key)
      } else {
        refresh().then(pump)
      }
    }
  }

  // Dopo ogni pagina, anche la prima, una dopo l'altra: l'avviso (o quello rimasto da una pagina
  // lasciata subito), le voci vecchie, la sezione delle foto (che si mostra quando ha ritrovato le
  // sue), poi la striscia e l'invio.
  async function onPage() {
    if (!document.getElementById("photo-queue")) return
    const marker = document.getElementById("intake-photos-saved")
    const saved = marker ? noteSaved(marker) : session.get(SAVED)
    if (saved) await applySaved(saved)
    await dropStale()
    const el = document.getElementById("intake-photos")
    intake = null
    if (el) await mountIntake(el)
    await refresh()
    pump()
  }
  let pages = Promise.resolve()
  document.addEventListener("gs:pagina", () => { pages = pages.then(onPage, onPage) })

  // La conferma prima di uscire resta solo per le foto in coda tenute in memoria.
  window.addEventListener("beforeunload", event => {
    if (!volatili.some(inAttesa)) return
    event.preventDefault()
    event.returnValue = ""
  })

  // Il giro fra la lista e «Nuova riparazione» senza ricaricare (slice 040b), solo finché ci sono voci
  // in memoria: altrimenti le pagine vanno come sempre, perché la coda è in IndexedDB.
  const attiva = () => volatili.length > 0
  const nelGiro = url => url.origin === location.origin && (url.pathname === "/" || url.pathname === "/nuova")

  // mostraNelGiro mostra la risposta di una richiesta del giro; se è il login (sessione finita) o una
  // pagina che non è del personale, va invece con una navigazione normale a voluto, così il server
  // ricorda la pagina di ritorno come senza JavaScript.
  function mostraNelGiro(risposta, html, voluto, cronologia) {
    const doc = leggiPagina(html)
    if (new URL(risposta.url).pathname === "/login" || !doc.getElementById("photo-queue")) {
      window.location.assign(voluto)
      return
    }
    mostraPagina(doc, risposta.url, cronologia)
  }

  function vai(indirizzo, cronologia) {
    fetch(indirizzo, {credentials: "same-origin", headers: {accept: "text/html"}})
      .then(risposta => risposta.text().then(html => mostraNelGiro(risposta, html, indirizzo, cronologia)))
      .catch(() => window.location.assign(indirizzo))
  }

  document.addEventListener("click", event => {
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
    const link = event.target.closest && event.target.closest("a[href]")
    if (!link || link.target || link.hasAttribute("download") || !attiva()) return
    const url = new URL(link.href, location.href)
    if (!nelGiro(url)) return
    event.preventDefault()
    vai(url.href, "aggiungi")
  })

  function inviaNelGiro(form) {
    fetch(form.action, {method: "POST", body: new URLSearchParams(new FormData(form)), credentials: "same-origin"})
      .then(risposta => risposta.text().then(html => mostraNelGiro(risposta, html, form.action, "sostituisci")))
      .catch(() => {
        delete form.dataset.inviato
        form.querySelectorAll("button[type=submit]").forEach(button => { button.disabled = false })
        connessioneAssente()
      })
  }

  // Le foto scelte la cui scrittura non è finita in tempo restano in memoria, come nel ripiego.
  function keepInMemory() {
    for (const entry of writes) {
      const item = entry.item
      if (!item || !item.pendingFile || item.volatile) continue
      item.volatile = true
      item.file = item.pendingFile
      if (!volatili.includes(item)) volatili.push(item)
    }
  }

  // «Genera busta»: si annota la chiave del modulo mandato (le sue foto, e solo quelle, passano alla
  // busta salvata). Le foto scelte devono essere salvate nel browser prima che la pagina cambi, quindi
  // se una scrittura o una cancellazione non è finita il modulo l'aspetta, al più WRITE_WAIT_MS, e poi
  // parte com'è (un invio normale). Con delle voci in memoria il modulo si manda con fetch, e la
  // pagina della risposta (l'esito dopo il redirect, o il modulo con gli errori) prende il posto di
  // questa. Il pulsante l'ha già spento data-invio-unico, sopra. Se la risposta è il login o non è una
  // pagina del personale (403 del CSRF), si apre «Nuova riparazione» con una navigazione normale.
  document.addEventListener("submit", event => {
    const form = event.target
    if (event.defaultPrevented || !(form instanceof HTMLFormElement) || form.id !== "new-repair-form") return
    if (stagedKey) session.set(SUBMITTED, {key: stagedKey})
    if (attiva()) {
      event.preventDefault()
      inviaNelGiro(form)
      return
    }
    if (writes.length === 0) return
    event.preventDefault()
    const timeout = new Promise(resolve => setTimeout(resolve, WRITE_WAIT_MS))
    Promise.race([Promise.allSettled(writes.map(entry => entry.promise)), timeout]).then(() => {
      keepInMemory()
      if (attiva()) inviaNelGiro(form)
      else HTMLFormElement.prototype.submit.call(form)
    })
  })

  // Avanti e indietro dopo una pagina cambiata qui: un salto d'ancora (stessa pagina, un altro hash)
  // non tocca niente; un'altra pagina nel giro con delle voci in memoria si cambia di nuovo,
  // altrimenti si carica.
  window.addEventListener("popstate", () => {
    if (!paginaCambiata) return
    const url = new URL(location.href)
    if (url.pathname + url.search === paginaMostrata) return
    if (nelGiro(url) && attiva()) vai(url.href, null)
    else window.location.reload()
  })
})()

// Il caricamento delle foto dalla scheda (slice 038): come auto_upload, il modulo parte appena si
// sceglie una foto (data-invio-automatico); senza JavaScript serve «Carica». Prima di partire fa il
// preflight di LiveView (put_entries e generate_preflight_response): più di 4 foto (D-59: lo
// standalone ne ammette 2) è «Massimo 4 foto per volta.», ogni foto si misura (oltre 25 MiB) e poi si
// guarda il formato (tipo o estensione), e partono solo le valide fra le prime 4. Gli errori viaggiano come campi
// «anteprima» in testa al modulo, e il server li mette nel riquadro: così una foto troppo grande non
// si manda nemmeno. Senza DataTransfer (browser vecchi) il modulo parte com'è e decide il server.
// Dalla slice 040b (RIP-S-29) il modulo parte con XHR, come l'upload di LiveView: nel riquadro
// #repair-photo-upload-progress ogni foto ha l'anteprima (un indirizzo blob: del browser, come
// live_img_preview), «<nome> · Caricamento foto: N%» e la barra; poi la pagina che il server dà dopo
// il redirect prende il posto di questa (mostraPagina), senza chiederla una seconda volta: il segno
// della scheda nel redirect è monouso. Una risposta che non è la scheda o la lista dopo il redirect (il
// login a sessione finita, un 403, 413 o 500) apre la scheda con una navigazione normale: il server
// la mostra (o porta al login ricordandola) come dopo un ricaricamento. Se l'invio non arriva al
// server, «Connessione assente». Mentre una foto viaggia, «Fotocamera» e «Galleria» sono spenti: una
// scelta nuova non si perde in silenzio, si fa quando l'invio è finito.
// «Foto caricata.» sparisce dopo 3 s (@photo_flash_timeout_ms): solo quel messaggio, mostrato da
// questa pagina, quindi un messaggio arrivato dopo, che è un'altra pagina, non si tocca.
;(function(){
  const MAX_FOTO = 4
  const MAX_BYTE = 25 * 1024 * 1024
  const TIPI = ["image/jpeg", "image/png", "image/webp"]
  const ESTENSIONI = [".jpg", ".jpeg", ".png", ".webp"]
  const estensione = nome => {
    const base = nome.slice(nome.lastIndexOf("/") + 1)
    const punto = base.lastIndexOf(".")
    return punto > 0 ? base.slice(punto).toLowerCase() : ""
  }
  const accettata = file => TIPI.includes(file.type) || ESTENSIONI.includes(estensione(file.name))
  document.addEventListener("change", event => {
    const input = event.target.closest && event.target.closest("input[type=file][data-invio-automatico]")
    if (!input || !input.files || input.files.length === 0 || !input.form) return
    const form = input.form
    form.querySelectorAll("input[name=anteprima]").forEach(campo => campo.remove())
    const files = Array.from(input.files)
    if (files.length > MAX_FOTO || files.some(file => file.size > MAX_BYTE)) {
      const codici = files.length > MAX_FOTO ? ["troppe"] : []
      files.forEach(file => {
        if (file.size > MAX_BYTE) codici.push("misura")
        else if (!accettata(file)) codici.push("formato")
      })
      const tenute = files.slice(0, MAX_FOTO).filter(file => file.size <= MAX_BYTE && accettata(file))
      try {
        const dt = new DataTransfer()
        tenute.forEach(file => dt.items.add(file))
        input.files = dt.files
        const primo = form.querySelector("input[type=file]")
        codici.forEach(codice => {
          const campo = document.createElement("input")
          campo.type = "hidden"
          campo.name = "anteprima"
          campo.value = codice
          form.insertBefore(campo, primo.closest("label") || primo)
        })
      } catch (_) {
        // Niente DataTransfer: il modulo parte com'è e il server decide da solo.
      }
    }
    invia(form, Array.from(input.files))
  })

  const elemento = (tag, attributi, testo) => {
    const nodo = document.createElement(tag)
    for (const [chiave, valore] of Object.entries(attributi)) nodo.setAttribute(chiave, valore)
    if (testo !== undefined) nodo.textContent = testo
    return nodo
  }

  function invia(form, files) {
    const riquadro = document.querySelector("#repair-photo-upload-progress")
    if (!riquadro) { form.requestSubmit(); return }
    if (form.dataset.inviato) return
    form.dataset.inviato = "1"
    const corpoInvio = new FormData(form)
    const comandi = document.querySelectorAll("#repair-photos .modulo-foto input[type=file], #repair-photos .modulo-foto button[type=submit]")
    comandi.forEach(c => { c.disabled = true })
    const righe = files.map(file => {
      const url = URL.createObjectURL(file)
      const riga = elemento("div", {class: "riga-avanzamento"})
      const testo = elemento("p", {class: "nome-foto"})
      const barra = elemento("progress", {value: "0", max: "100", "aria-label": "Avanzamento foto"})
      const scrivi = percento => {
        testo.textContent = `${file.name} · Caricamento foto: ${percento}%`
        barra.value = percento
      }
      const corpo = elemento("div", {class: "avanzamento-corpo"})
      corpo.append(testo, barra)
      riga.append(elemento("img", {src: url, alt: `Anteprima di ${file.name}`, class: "anteprima-foto"}), corpo)
      riquadro.append(riga)
      scrivi(0)
      return {file, url, scrivi}
    })
    const libera = () => righe.forEach(r => URL.revokeObjectURL(r.url))
    const xhr = new XMLHttpRequest()
    xhr.open("POST", form.action)
    // L'avanzamento di ogni foto, nell'ordine in cui le foto viaggiano nel corpo.
    xhr.upload.onprogress = event => {
      if (!event.lengthComputable) return
      let resto = event.loaded * righe.reduce((n, r) => n + r.file.size, 0) / event.total
      for (const r of righe) {
        const parte = r.file.size > 0 ? Math.min(1, Math.max(0, resto / r.file.size)) : 1
        r.scrivi(Math.floor(parte * 100))
        resto -= r.file.size
      }
    }
    const scheda = new URL(form.action, window.location.href).pathname.replace(/\/foto$/, "")
    xhr.onload = () => {
      libera()
      let arrivo = ""
      try { arrivo = new URL(xhr.responseURL).pathname } catch (_) { arrivo = "" }
      const tipo = xhr.getResponseHeader("content-type") || ""
      if (xhr.status === 200 && tipo.startsWith("text/html") && (arrivo === scheda || arrivo === "/")) {
        const doc = leggiPagina(xhr.responseText)
        if (doc.getElementById("photo-queue")) return mostraPagina(doc, xhr.responseURL, "sostituisci", true)
      }
      window.location.assign(scheda)
    }
    xhr.onerror = xhr.onabort = xhr.ontimeout = () => {
      libera()
      riquadro.replaceChildren()
      delete form.dataset.inviato
      comandi.forEach(c => { c.disabled = false })
      connessioneAssente()
    }
    xhr.send(corpoInvio)
  }
})()
document.addEventListener("gs:pagina", () => {
  document.querySelectorAll(".flash-info").forEach(flash => {
    if (flash.textContent.trim() === "Foto caricata.") setTimeout(() => flash.remove(), 3000)
  })
})
// Il riquadro della firma di consegna (RIP-S-37, slice 047): signature_pad.js dello standalone, uguale.
// Non è una comodità: una firma col dito non ha un equivalente senza JavaScript (la pagina lo dice), e
// il modulo non deve mai mandare un riquadro vuoto, quindi l'invio è un cancello. La firma esiste dal
// primo tratto, non dal primo tocco: un tocco solo non disegna e non firma (la sorgente la segnava al
// pointerdown, e un tocco per sbaglio mandava un PNG tutto trasparente). Delegato dal documento e
// legato una volta sola: vale anche per una pagina mostrata senza ricaricare.
;(function(){
  const PAD = "signature-pad"
  const FORM = "signature-form"
  const FIELD = "signature-png"
  const CLEAR = "signature-clear"

  let state = null

  const pad = () => document.getElementById(PAD)

  const clearField = () => {
    const field = document.getElementById(FIELD)
    if (field) field.value = ""
  }

  // Il canvas ha la misura del CSS e i pixel del dispositivo, come syncCanvas della sorgente. Cambiare
  // misura lo svuota, quindi un riquadro su cui si è già firmato resta com'è: girare il telefono non
  // fa perdere la firma.
  const fit = () => {
    const canvas = pad()
    if (!canvas) return null
    if (state && state.canvas === canvas && !state.empty) return state

    const box = canvas.getBoundingClientRect()
    const ratio = window.devicePixelRatio || 1
    canvas.width = Math.max(1, Math.round(box.width * ratio))
    canvas.height = Math.max(1, Math.round(box.height * ratio))

    const context = canvas.getContext("2d")
    context.setTransform(ratio, 0, 0, ratio, 0, 0)
    context.lineWidth = 2.5
    context.lineCap = "round"
    context.strokeStyle = "#102a6b"

    state = {canvas, context, empty: true, drawing: false}
    return state
  }

  const point = (canvas, event) => {
    const box = canvas.getBoundingClientRect()
    return [event.clientX - box.left, event.clientY - box.top]
  }

  document.addEventListener("pointerdown", event => {
    const canvas = event.target.closest && event.target.closest(`#${PAD}`)
    if (!canvas) return
    const current = fit()
    if (!current) return

    current.drawing = true
    const [x, y] = point(canvas, event)
    current.context.beginPath()
    current.context.moveTo(x, y)
    canvas.setPointerCapture(event.pointerId)
    event.preventDefault()
  })

  document.addEventListener("pointermove", event => {
    if (!state || !state.drawing) return
    const [x, y] = point(state.canvas, event)
    state.context.lineTo(x, y)
    state.context.stroke()
    state.empty = false
    event.preventDefault()
  })

  for (const name of ["pointerup", "pointercancel"]) {
    document.addEventListener(name, () => {
      if (state) state.drawing = false
    })
  }

  document.addEventListener("click", event => {
    const button = event.target.closest && event.target.closest(`#${CLEAR}`)
    if (!button) return
    const current = state || fit()
    if (!current) return

    current.context.setTransform(1, 0, 0, 1, 0, 0)
    current.context.clearRect(0, 0, current.canvas.width, current.canvas.height)
    current.empty = true
    state = null
    fit()
    // Il campo tiene quello che un invio rifiutato ci ha lasciato: «Pulisci» svuota anche lui,
    // altrimenti il modulo avrebbe ancora la firma di prima mentre il riquadro è bianco.
    clearField()
  })

  window.addEventListener("resize", () => {
    if (state && state.empty) {
      state = null
      fit()
    }
  })

  document.addEventListener("submit", event => {
    const form = event.target
    if (!form || form.id !== FORM) return

    const field = document.getElementById(FIELD)
    const canvas = pad()

    if (!field || !canvas || !state || state.empty) {
      event.preventDefault()
      clearField()
      window.alert("Firma mancante.")
      return
    }

    field.value = canvas.toDataURL("image/png")
  })
})()

// Il ritorno alla lista filtrata (RIP-H-20, slice 056): .ListReturnStore e .ListReturn dello
// standalone (slice 187). La lista scrive il suo indirizzo (#list-return, data-path, deciso dal
// server: filtri e pagina, mai un codice d'etichetta) in sessionStorage, e «← Indietro» della scheda e
// di «Nuova riparazione» (data-list-return) ci porta, purché sia l'indirizzo di una lista: «/» da solo
// o con una query di caratteri da URL. Senza storage (finestra privata) o senza JavaScript resta «/».
;(() => {
  const CHIAVE = "gestrip:list-return"
  const LISTA = /^\/(\?[A-Za-z0-9\-._~!$&'()*+,;=:@%\/?]*)?$/
  document.addEventListener("gs:pagina", () => {
    const qui = document.getElementById("list-return")
    if (qui) {
      try {
        sessionStorage.setItem(CHIAVE, qui.dataset.path)
      } catch (_e) {
        // storage spento: «← Indietro» resta «/»
      }
    }
    const indietro = document.querySelectorAll("a[data-list-return]")
    if (indietro.length === 0) return
    let percorso = null
    try {
      percorso = sessionStorage.getItem(CHIAVE)
    } catch (_e) {
      return
    }
    if (percorso && LISTA.test(percorso)) indietro.forEach(a => a.setAttribute("href", percorso))
  })
})()

// Le azioni del riquadro della lista (slice 056, RIP-H-14, 15, 16): senza JavaScript sono moduli POST
// che tornano alla lista (lista_azioni.go), e il menu dello stato ha il pulsante «Cambia stato». Qui il menu
// parte appena si sceglie (come phx-change), «Cambia stato» non si vede (data-solo-senza-js), e i moduli
// (data-azione-lista) partono con fetch: la lista di ritorno prende il posto di questa allo stesso
// punto della pagina, come la lista di LiveView che si aggiorna sul posto, e il suo messaggio resta
// in cima allo schermo finché non lo si tocca (flash-sospesa). «✓ Pronta» porta alla scheda sul
// prezzo, con la cronologia. La conferma di «✓ Consegnata» è quella di data-confirm, sopra. Un
// secondo invio dello stesso modulo, finché il primo non ha risposto, non parte (un doppio tocco sul
// 📌 lo toglierebbe di nuovo). Il login (sessione finita) ricarica la pagina; una risposta che non è
// una pagina del personale (il 403 del CSRF) si mostra com'è.
;(() => {
  document.addEventListener("gs:pagina", () => {
    document.querySelectorAll("[data-solo-senza-js]").forEach(el => { el.hidden = true })
  })
  document.addEventListener("change", event => {
    const menu = event.target
    if (!(menu instanceof HTMLSelectElement) || !menu.form || !menu.form.hasAttribute("data-stato-lista")) return
    menu.form.requestSubmit()
  })
  document.addEventListener("submit", event => {
    const form = event.target
    if (event.defaultPrevented || !(form instanceof HTMLFormElement) || !form.hasAttribute("data-azione-lista")) return
    event.preventDefault()
    if (form.dataset.inviato) return
    form.dataset.inviato = "1"
    fetch(form.action, {method: "POST", body: new URLSearchParams(new FormData(form)), credentials: "same-origin", headers: {accept: "text/html"}})
      .then(r => r.text().then(html => {
        const doc = leggiPagina(html)
        if (new URL(r.url).pathname === "/login") { window.location.reload(); return }
        if (!doc.getElementById("photo-queue")) {
          document.open()
          document.write(html)
          document.close()
          return
        }
        const sullaLista = new URL(r.url).pathname === "/"
        mostraPagina(doc, r.url, sullaLista ? "sostituisci" : "aggiungi", sullaLista)
        if (sullaLista) document.querySelectorAll("body > .flash").forEach(flash => flash.classList.add("flash-sospesa"))
      }))
      .catch(() => {
        delete form.dataset.inviato
        connessioneAssente()
      })
  })
  document.addEventListener("click", event => {
    const flash = event.target.closest && event.target.closest(".flash-sospesa")
    if (flash) flash.remove()
  })
})()

paginaPronta()

// Una miniatura della galleria che non arriva (un momento in cui il server è occupato) si chiede
// un'altra volta dopo un attimo invece di lasciare un quadrato vuoto (slice 057, img_retry.js dello
// standalone, slice 205): solo le immagini con data-retry, una volta ciascuna. L'evento error non
// risale: si ascolta nella fase di cattura, una volta per la pagina.
;(() => {
  document.addEventListener("error", event => {
    const img = event.target
    if (!img || img.tagName !== "IMG" || !img.hasAttribute("data-retry") || img.hasAttribute("data-retried")) return
    img.setAttribute("data-retried", "")
    const src = img.getAttribute("src")
    setTimeout(() => { img.src = src + (src.includes("?") ? "&" : "?") + "retry=1" }, 1500)
  }, true)
})()
