package clientip

import (
	"net/http"
	"strings"
	"testing"
)

func richiesta(peer string, xff ...string) *http.Request {
	r, _ := http.NewRequest("GET", "/", nil)
	r.RemoteAddr = peer
	for _, v := range xff {
		r.Header.Add("X-Forwarded-For", v)
	}
	return r
}

// inventario: CLI-21
// I casi di «the client address read from the forwarded chain» (attempt_limits_test.exs): la
// voce più a destra non privata di X-Forwarded-For, creduta solo da un peer privato; IPv4-mapped
// come IPv4; IPv6 per /64; solo voci private → l'ultima; niente intestazione → il peer; un peer
// pubblico è sé stesso; una voce che non è un indirizzo conta per quello che dice.
func TestDiComeLoStandalone(t *testing.T) {
	const mappato = "[::ffff:172.18.0.3]:51000"
	for _, c := range []struct {
		peer   string
		xff    []string
		atteso string
	}{
		{mappato, []string{"198.51.100.40, 172.20.0.1"}, "198.51.100.40"},
		{"172.18.0.3:51000", []string{"198.51.100.40, 172.20.0.1"}, "198.51.100.40"},
		{mappato, []string{"::ffff:198.51.100.41, 172.20.0.1"}, "198.51.100.41"},
		{mappato, []string{"2001:db8:1:2:aaaa::7, 172.20.0.1"}, "2001:db8:1:2::/64"},
		{mappato, []string{"2001:db8:1:2:bbbb::9, 172.20.0.1"}, "2001:db8:1:2::/64"},
		{mappato, []string{"192.168.1.20, 172.20.0.1"}, "172.20.0.1"},
		{mappato, nil, "172.18.0.3"},
		{"203.0.113.9:443", []string{"198.51.100.40"}, "203.0.113.9"},
		{"[::ffff:203.0.113.9]:443", []string{"198.51.100.40"}, "203.0.113.9"},
		{mappato, []string{"nonsense, 172.20.0.1"}, "nonsense"},
		// In più, dalle regole lette in client_ip.ex.
		{"127.0.0.1:40000", []string{"198.51.100.7, 198.51.100.8"}, "198.51.100.8"},   // la più a destra
		{"127.0.0.1:40000", []string{" , 198.51.100.7 ,, "}, "198.51.100.7"},          // vuote tolte
		{"127.0.0.1:40000", []string{" , "}, "127.0.0.1"},                             // nessuna voce: il peer
		{"127.0.0.1:40000", []string{"198.51.100.7", "198.51.100.9"}, "198.51.100.7"}, // solo la prima intestazione
		{"[::1]:40000", []string{"198.51.100.7"}, "198.51.100.7"},
		{"[fd12::5]:40000", []string{"198.51.100.7"}, "198.51.100.7"},
		{"[fc00::5]:40000", []string{"198.51.100.7"}, "198.51.100.7"},
		{"10.1.2.3:1", []string{"198.51.100.7, 10.0.0.1, 192.168.0.1, 172.31.0.1, 127.0.0.2, ::1, fd00::1"}, "198.51.100.7"},
		{"10.1.2.3:1", []string{"198.51.100.7, 172.32.0.1"}, "172.32.0.1"}, // fuori da 172.16/12
		{"10.1.2.3:1", []string{"198.51.100.7, 100.64.0.1"}, "100.64.0.1"}, // la tailnet non è privata (PIANO §1.6)
		{"100.64.0.1:1", []string{"198.51.100.7"}, "100.64.0.1"},           // nemmeno come peer
		{"[fe80::1]:1", []string{"198.51.100.7"}, "fe80::/64"},             // link-local non è privato
		{"[2001:db8::1]:1", nil, "2001:db8::/64"},
		{"[fe00::1]:1", []string{"198.51.100.7"}, "fe00::/64"},
		// La tailnet non è privata (PIANO §1.6), nemmeno su IPv6: i nodi di una tailnet hanno lo
		// stesso /64 (fd7a:115c:a1e0:…), quindi contano per indirizzo intero (review della 009).
		{"127.0.0.1:1", []string{"fd7a:115c:a1e0:ab12:4843:cd96:6258:b240"}, "fd7a:115c:a1e0:ab12:4843:cd96:6258:b240"},
		{"127.0.0.1:1", []string{"fd7a:115c:a1e0:ab12:4843:cd96:6263:1111"}, "fd7a:115c:a1e0:ab12:4843:cd96:6263:1111"},
		{"127.0.0.1:1", []string{"198.51.100.7, fd7a:115c:a1e0::1"}, "fd7a:115c:a1e0::1"},
		{"[fd7a:115c:a1e0::5]:1", []string{"198.51.100.7"}, "fd7a:115c:a1e0::5"},     // vicino della tailnet: non creduto
		{"127.0.0.1:1", []string{"198.51.100.7, fd7a:115c:a1e1::1"}, "198.51.100.7"}, // fuori dal /48: privato
		// Una voce lunga che non è un indirizzo: i primi 64 caratteri (String.slice/3).
		{"127.0.0.1:1", []string{strings.Repeat("è", 70)}, strings.Repeat("è", 64)},
		{"127.0.0.1:1", []string{strings.Repeat("x", 64)}, strings.Repeat("x", 64)},
	} {
		if got := Di(richiesta(c.peer, c.xff...)); got != c.atteso {
			t.Errorf("peer %s, XFF %q → %q, atteso %q", c.peer, c.xff, got, c.atteso)
		}
	}
}
