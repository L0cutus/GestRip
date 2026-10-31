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

// «Togli data» (slice 158 dello standalone): svuota il campo data nominato da data-clear-field e
// lo dice al modulo, perché il selettore delle date dei telefoni non permette di svuotarlo. Nello
// standalone è JS.dispatch("gs:clear-field"); senza JavaScript il pulsante non fa niente, come là.
document.addEventListener("click", event => {
  const button = event.target.closest && event.target.closest("[data-clear-field]")
  if (!button) return
  const input = document.getElementById(button.getAttribute("data-clear-field"))
  if (!input) return
  input.value = ""
  input.dispatchEvent(new Event("input", {bubbles: true}))
})

// Un invio solo per i moduli segnati data-invio-unico (la nuova busta), come phx-submit di
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
