// Package clientip dà l'indirizzo con cui si conta un cliente (CLI-21, ACC-20): le regole di
// ClientIp dello standalone (slice 208). È una chiave per contare e una riga di registro, mai
// un'autorizzazione.
//
// L'intestazione X-Forwarded-For si legge solo se il vicino è su loopback o su una rete privata:
// davanti al Go c'è tailscaled su 127.0.0.1 (PIANO §1.6). Il cliente è la voce **più a destra che
// non è privata**: ogni proxy accoda il vicino che ha visto, e quelli privati sono i nostri. Gli
// indirizzi della tailnet (100.64.0.0/10 e fd7a:115c:a1e0::/48) non sono privati: un membro della
// tailnet conta come chiunque (PIANO §1.6). Un IPv4-mapped si legge come IPv4; un cliente IPv6
// conta per il suo /64, tranne un nodo della tailnet, che conta per indirizzo intero perché tutti
// i nodi di una tailnet hanno lo stesso /64 (review della 009; da confermare dal vivo in T1). Senza
// l'intestazione, o con sole voci private, l'ultima voce o il vicino.
//
// Da provare dal vivo dietro Tailscale Funnel (T1): quale intestazione arriva davvero.
package clientip

import (
	"net/http"
	"net/netip"
	"strings"
)

// Di è la chiave del cliente di r.
func Di(r *http.Request) string {
	vicino, ok := indirizzo(r.RemoteAddr)
	if !ok {
		return primi64(r.RemoteAddr)
	}
	if privato(vicino) {
		// Solo la prima intestazione, come get_req_header/2 con [value | _].
		if v := r.Header.Values("X-Forwarded-For"); len(v) > 0 {
			if voci := voci(v[0]); len(voci) > 0 {
				for i := len(voci) - 1; i >= 0; i-- {
					if !voci[i].privata {
						return voci[i].chiave
					}
				}
				return voci[len(voci)-1].chiave
			}
		}
	}
	return chiave(vicino)
}

// indirizzo legge l'indirizzo del vicino da «ip:porta».
func indirizzo(remoto string) (netip.Addr, bool) {
	ap, err := netip.ParseAddrPort(remoto)
	if err != nil {
		return netip.Addr{}, false
	}
	return ap.Addr().Unmap(), true
}

type voce struct {
	chiave  string
	privata bool
}

// voci divide l'intestazione: spazi tolti, voci vuote scartate; quello che non è un indirizzo
// resta com'è, nei primi 64 caratteri, e non è privato.
func voci(valore string) []voce {
	var out []voce
	for _, v := range strings.Split(valore, ",") {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		a, err := netip.ParseAddr(v)
		if err != nil || a.Zone() != "" {
			out = append(out, voce{chiave: primi64(v)})
			continue
		}
		a = a.Unmap()
		out = append(out, voce{chiave: chiave(a), privata: privato(a)})
	}
	return out
}

func primi64(s string) string {
	r := []rune(s)
	if len(r) > 64 {
		r = r[:64]
	}
	return string(r)
}

// tailnetIPv6 è il prefisso IPv6 degli indirizzi Tailscale.
var tailnetIPv6 = netip.MustParsePrefix("fd7a:115c:a1e0::/48")

// chiave: un IPv4 com'è, un IPv6 come il suo /64 («2001:db8:1:2::/64», come :inet.ntoa); un nodo
// della tailnet per indirizzo intero.
func chiave(a netip.Addr) string {
	if a.Is4() || tailnetIPv6.Contains(a) {
		return a.String()
	}
	p, _ := a.Prefix(64) // un IPv6 senza zona: non fallisce
	return p.Addr().String() + "/64"
}

// privato: 127/8, 10/8, 172.16/12, 192.168/16, ::1 e fc00::/7, come private?/1 dello standalone;
// la tailnet no (100.64/10 non è nell'elenco, fd7a:115c:a1e0::/48 si toglie da fc00::/7).
func privato(a netip.Addr) bool {
	if tailnetIPv6.Contains(a) {
		return false
	}
	if a.Is4() {
		b := a.As4()
		return b[0] == 127 || b[0] == 10 || (b[0] == 172 && b[1]&0xf0 == 16) || (b[0] == 192 && b[1] == 168)
	}
	b := a.As16()
	return a == netip.IPv6Loopback() || b[0]&0xfe == 0xfc
}
