//go:build !nodynamic

package imaging

// Senza -tags nodynamic gen2brain/webp compila anche il caricamento di libwebp di sistema
// (purego): il binario diventa dinamico (misurato: «dynamically linked», con l'interprete
// /lib64/ld-linux) e, dove c'è libwebp.so, scrive le miniature con quella. Il tema H vuole un
// binario statico con il libwebp tradotto in Go, quindi senza il tag non si compila.
var _ = serveIlTagNodynamic // compilare con -tags nodynamic (docs/SVILUPPO.md)
