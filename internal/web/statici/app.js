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

// Il caricamento delle foto dalla scheda (slice 038): come auto_upload, il modulo parte appena si
// sceglie una foto (data-invio-automatico); senza JavaScript serve «Carica». Prima di partire fa il
// preflight di LiveView (put_entries e generate_preflight_response): più di 2 foto è «Massimo 2
// foto per volta.», ogni foto si misura (oltre 25 MiB) e poi si guarda il formato (tipo o
// estensione), e partono solo le valide fra le prime 2. Gli errori viaggiano come campi
// «anteprima» in testa al modulo, e il server li mette nel riquadro: così una foto troppo grande non
// si manda nemmeno. Senza DataTransfer (browser vecchi) il modulo parte com'è e decide il server.
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
    form.requestSubmit()
  })
})()
document.querySelectorAll(".flash-info").forEach(flash => {
  if (flash.textContent.trim() === "Foto caricata.") setTimeout(() => flash.remove(), 3000)
})
