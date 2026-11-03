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
// chiedere. Sta prima di data-invio-unico, che spegne il pulsante solo a invio partito.
document.addEventListener("click", event => {
  const el = event.target.closest && event.target.closest("[data-confirm]")
  if (!el) return
  if (!window.confirm(el.getAttribute("data-confirm"))) {
    event.preventDefault()
    event.stopImmediatePropagation()
  }
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

// Le pagine cambiate senza ricaricare (slice 040b). La coda delle foto di «Nuova riparazione» vive in
// questo documento, come in photo_queue.js dello standalone, dove le pagine del personale sono
// navigazioni di LiveView che non ricaricano. Qui ogni pagina è un documento: per non perdere le
// foto, finché la coda ha qualcosa da fare app.js porta da sé il giro fra la lista e «Nuova
// riparazione» (i link a / e /nuova e «Genera busta»), chiedendo la pagina al server e mettendone
// il body al posto di questo; la striscia #photo-queue resta la stessa. Anche il caricamento dalla
// scheda (sotto) mostra così la pagina che il server dà dopo il redirect, senza chiederla due volte.
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

// La coda delle foto scelte prima di generare la busta (RIP-H-40, slice 040b): photo_queue.js dello
// standalone (slice 190), riga per riga. «Nuova riparazione» ha i due pulsanti, ma le foto scelte
// restano nel browser (con le anteprime) finché «Genera busta» non ha salvato la busta. La pagina
// dell'esito porta allora l'avviso #intake-photos-saved con id e numero della busta nuova e la
// chiave del modulo salvato (l'evento intake-photos-saved dello standalone), e le foto passano alla
// coda sotto, che le manda una per richiesta a POST /repairs/{id}/photos (XHR, per la barra) mentre
// si compila già la busta dopo. Una pagina ricaricata o chiusa perde le foto, quindi con delle foto in
// coda la pagina chiede prima di uscire. Le foto partono come sono, mai ridimensionate né
// ricompresse qui: il server le tiene in JPEG alla risoluzione del telefono.
;(function(){
  // MediaStore.max_input_bytes/0 sul server, che sopra risponde 413.
  const MAX_BYTES = 25 * 1024 * 1024
  const PER_SOURCE = 2
  const TYPES = {".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".webp": "image/webp"}
  const MIMES = new Set(Object.values(TYPES))

  // Le foto scelte nel modulo sullo schermo, nell'ordine in cui sono state scelte: la prima di una
  // busta senza foto diventa la foto della busta.
  const staged = []
  // La chiave del modulo a cui appartengono (data-intake-key, disegnata dal server, nuova per ogni
  // modulo e per ogni busta salvata). Le foto vanno solo alla busta salvata con questa chiave.
  let stagedKey = null
  const DROPPED = "Le foto scelte prima sono state tolte: scegli di nuovo quelle di questa busta."
  // Una voce per busta che aspetta le sue foto: {repairId, bag, files, done, part, error, rejected}.
  const jobs = []
  let sending = null
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

  function clearStaged() {
    staged.splice(0).forEach(photo => URL.revokeObjectURL(photo.url))
  }

  function element(tag, attributes = {}, text = null) {
    const node = document.createElement(tag)
    for (const [key, value] of Object.entries(attributes)) node.setAttribute(key, value)
    if (text !== null) node.textContent = text
    return node
  }

  // mounted() dell'hook: un modulo con un'altra chiave è un'altra busta, e quello che aveva scelto un
  // modulo lasciato non sono le sue foto (lo si dice). Lo stesso modulo ridisegnato dal server (avviso
  // del QR, salvataggio rifiutato) ha la stessa chiave e tiene le foto, come la sezione con
  // phx-update="ignore" dello standalone.
  function mountIntake(el) {
    el.hidden = false
    const same = el.dataset.intakeKey === stagedKey
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
    if (same) return drawIntake(lastErrors)
    const dropped = staged.length > 0
    clearStaged()
    stagedKey = el.dataset.intakeKey
    drawIntake(dropped ? [DROPPED] : [])
  }

  function pick(input, source) {
    const files = Array.from(input.files || [])
    // Svuotato subito, così la stessa foto si può scegliere di nuovo dopo averla tolta.
    input.value = ""
    if (files.length === 0) return
    const held = staged.filter(photo => photo.source === source).length
    if (held + files.length > PER_SOURCE) return drawIntake(["Massimo 2 foto per volta."])
    const errors = []
    for (const file of files) {
      const problem = refusal(file)
      if (problem) errors.push(`${file.name}: ${problem}`)
      else staged.push({file, source, url: URL.createObjectURL(file)})
    }
    drawIntake(errors)
  }

  function remove(photo) {
    const index = staged.indexOf(photo)
    if (index >= 0) staged.splice(index, 1)
    URL.revokeObjectURL(photo.url)
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
          src: photo.url,
          alt: `Anteprima di ${photo.file.name}`,
          class: "anteprima-foto",
        }),
        element("span", {class: "nome-foto"}, photo.file.name),
      )
      const button = element("button", {
        type: "button",
        class: "pulsante-secondario",
        "aria-label": `Rimuovi ${photo.file.name}`,
      }, "Rimuovi")
      button.addEventListener("click", () => remove(photo))
      row.append(button)
      intake.status.append(row)
    }
    intake.errors.replaceChildren(...errors.map(message => element("p", {}, message)))
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

  function drop(job) {
    const index = jobs.indexOf(job)
    if (index >= 0) jobs.splice(index, 1)
  }

  // Quello che il server ha rifiutato per com'è la foto (misura, tipo, non un'immagine) sarebbe
  // rifiutato di nuovo: quella foto si mette da parte e si va avanti con la prossima della busta.
  // Tutto il resto (connessione, sessione, server) può passare riprovando: la busta aspetta «Riprova».
  const PERMANENT = new Set([413, 415, 422])

  function finish(job, error, status = 0) {
    sending = null
    if (error && !PERMANENT.has(status)) {
      job.error = error
    } else {
      if (error) job.rejected.push(error)
      job.done += 1
      job.part = 0
      if (job.done >= job.files.length && job.rejected.length === 0) drop(job)
    }
    draw()
    pump()
  }

  function pending(job) { return job.done < job.files.length }

  function send(job) {
    const file = job.files[job.done]
    const xhr = new XMLHttpRequest()
    sending = job
    xhr.open("POST", `/repairs/${encodeURIComponent(job.repairId)}/photos`)
    xhr.setRequestHeader("x-csrf-token", csrfToken())
    xhr.setRequestHeader("content-type", photoType(file))
    xhr.setRequestHeader("x-file-name", encodeURIComponent((file.name || "").slice(-200)))
    // Un caricamento fermo da telefono deve finire, o ogni busta dietro aspetta per sempre.
    xhr.timeout = 300000
    xhr.upload.onprogress = event => {
      if (!event.lengthComputable) return
      job.part = event.loaded / event.total
      draw()
    }
    xhr.onload = () => finish(job, xhr.status === 201 ? null : reason(xhr), xhr.status)
    xhr.onerror = () => finish(job, "connessione assente")
    xhr.onabort = () => finish(job, "invio interrotto")
    xhr.ontimeout = () => finish(job, "tempo scaduto")
    xhr.send(file)
  }

  // Una foto alla volta, le buste nell'ordine in cui sono nate. Una foto non riuscita trattiene le foto
  // dopo della sua busta (prenderebbero il suo posto di foto della busta), mai quelle di un'altra.
  function pump() {
    if (sending) return
    const job = jobs.find(candidate => !candidate.error && pending(candidate))
    if (job) send(job)
  }

  function enqueue(repairId, bag, files) {
    jobs.push({repairId, bag, files, done: 0, part: 0, error: null, rejected: []})
    draw()
    pump()
  }

  function draw() {
    const strip = document.getElementById("photo-queue")
    if (!strip) return
    strip.hidden = jobs.length === 0
    strip.replaceChildren(...jobs.map(job => {
      const total = job.files.length
      const row = element("div", {
        "data-repair-id": String(job.repairId),
        class: "riga-coda" + (job.error ? " riga-coda-errore" : ""),
      })
      const close = () => {
        const button = element("button", {
          type: "button",
          class: "chiudi-coda",
          "aria-label": `Lascia perdere le foto della busta ${job.bag}`,
        }, "✕")
        button.addEventListener("click", () => { drop(job); draw() })
        return button
      }
      const sent = job.done - job.rejected.length
      if (job.error) {
        row.setAttribute("role", "alert")
        row.append(element("span", {class: "testo-coda"},
          `❌ Busta ${job.bag}: ${sent}/${total} foto caricate, ${job.error}.`))
        const retry = element("button", {type: "button", class: "riprova-coda"}, "Riprova")
        retry.addEventListener("click", () => { job.error = null; draw(); pump() })
        row.append(retry, close())
      } else if (!pending(job)) {
        // Finita, ma con foto che il server ha rifiutato: lo dice finché non la si chiude.
        row.setAttribute("role", "alert")
        const refused = job.rejected.length === 1 ? "1 rifiutata" : `${job.rejected.length} rifiutate`
        row.append(element("span", {class: "testo-coda"},
          `❌ Busta ${job.bag}: ${sent}/${total} foto caricate, ${refused} (${[...new Set(job.rejected)].join(", ")}).`), close())
      } else {
        const percent = Math.round(((job.done + job.part) / total) * 100)
        row.append(
          element("span", {class: "testo-coda"}, `📷 Busta ${job.bag}: ${sent}/${total} foto, ${percent}%`),
          element("progress", {value: String(percent), max: "100", "aria-label": `Foto della busta ${job.bag}`}),
        )
      }
      return row
    }))
  }

  // L'avviso della busta salvata (l'evento intake-photos-saved): le foto scelte vanno alla coda solo
  // se la busta è stata salvata con la chiave sotto cui sono state scelte; in ogni caso non sono più
  // di nessun modulo.
  function saved(marker) {
    const files = staged.map(photo => photo.file)
    const key = stagedKey
    clearStaged()
    const {repairId, bagNumber: bag, intakeKey: savedKey} = marker.dataset
    marker.remove()
    if (files.length > 0 && repairId && savedKey === key) enqueue(repairId, bag, files)
  }

  // Dopo ogni pagina: l'avviso, poi la sezione delle foto, poi la striscia (una pagina caricata da
  // capo ha la striscia vuota).
  document.addEventListener("gs:pagina", () => {
    const marker = document.getElementById("intake-photos-saved")
    if (marker) saved(marker)
    const el = document.getElementById("intake-photos")
    intake = null
    if (el) mountIntake(el)
    draw()
  })

  window.addEventListener("beforeunload", event => {
    if (!jobs.some(pending)) return
    event.preventDefault()
    event.returnValue = ""
  })

  // Il giro fra la lista e «Nuova riparazione» senza ricaricare, solo finché la coda o il modulo
  // hanno delle foto: altrimenti le pagine vanno come sempre.
  const attiva = () => staged.length > 0 || jobs.length > 0
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

  // «Genera busta»: con delle foto il modulo si manda con fetch, e la pagina della risposta (l'esito
  // dopo il redirect, o il modulo con gli errori) prende il posto di questa. Il pulsante l'ha già
  // spento data-invio-unico, sopra. Se la risposta è il login o non è una pagina del personale (403
  // del CSRF), si apre «Nuova riparazione» con una navigazione normale.
  document.addEventListener("submit", event => {
    const form = event.target
    if (event.defaultPrevented || !(form instanceof HTMLFormElement) || form.id !== "new-repair-form" || !attiva()) return
    event.preventDefault()
    fetch(form.action, {method: "POST", body: new URLSearchParams(new FormData(form)), credentials: "same-origin"})
      .then(risposta => risposta.text().then(html => mostraNelGiro(risposta, html, form.action, "sostituisci")))
      .catch(() => {
        delete form.dataset.inviato
        form.querySelectorAll("button[type=submit]").forEach(button => { button.disabled = false })
        connessioneAssente()
      })
  })

  // Avanti e indietro dopo una pagina cambiata qui: un salto d'ancora (stessa pagina, un altro hash)
  // non tocca niente; un'altra pagina nel giro con la coda attiva si cambia di nuovo, altrimenti si
  // carica.
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
// preflight di LiveView (put_entries e generate_preflight_response): più di 2 foto è «Massimo 2
// foto per volta.», ogni foto si misura (oltre 25 MiB) e poi si guarda il formato (tipo o
// estensione), e partono solo le valide fra le prime 2. Gli errori viaggiano come campi
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
  const MAX_FOTO = 2
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
paginaPronta()
