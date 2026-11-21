package sidecar

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// radiceProc è /proc (i test non la cambiano: leggono i processi veri).
const radiceProc = "/proc"

// statoListen è lo stato TCP_LISTEN in /proc/net/tcp.
const statoListen = "0A"

// locale4 è 127.0.0.1 come lo scrive /proc/net/tcp: la parola di 32 bit in rete letta nell'ordine
// dei byte della macchina, in esadecimale a 8 cifre.
var locale4 = fmt.Sprintf("%08X", binary.NativeEndian.Uint32([]byte{127, 0, 0, 1}))

// ascoltoDelGruppo dice se sulla porta c'è almeno un socket TCP in ascolto, se tutti i socket in
// ascolto sulla porta sono su 127.0.0.1 (IPv4) e se ognuno è aperto da un processo del gruppo. Il
// motivo del no è un genere fisso, per il registro.
func ascoltoDelGruppo(porta, gruppo int) (bool, string) {
	inode4, altri4, err := socketInAscolto(radiceProc+"/net/tcp", porta, true)
	if err != nil {
		return false, "lettura di /proc/net/tcp"
	}
	_, altri6, err := socketInAscolto(radiceProc+"/net/tcp6", porta, false)
	if err != nil && !os.IsNotExist(err) { // senza IPv6 il file non c'è
		return false, "lettura di /proc/net/tcp6"
	}
	if altri4+altri6 > 0 {
		return false, "in ascolto anche fuori da 127.0.0.1"
	}
	if len(inode4) == 0 {
		return false, "nessun socket in ascolto"
	}
	suoi := socketDelGruppo(gruppo)
	for _, i := range inode4 {
		if !suoi[i] {
			return false, "socket di un altro processo"
		}
	}
	return true, ""
}

// socketInAscolto legge un file /proc/net/tcp*: gli inode dei socket in ascolto sulla porta su
// 127.0.0.1 (solo con locale) e quanti altri ascoltano sulla porta.
func socketInAscolto(file string, porta int, locale bool) (inode []string, altri int, err error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, 0, err
	}
	hexPorta := fmt.Sprintf("%04X", porta)
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Scan() // intestazione
	for sc.Scan() {
		c := strings.Fields(sc.Text())
		if len(c) < 10 || c[3] != statoListen {
			continue
		}
		ind, p, ok := strings.Cut(c[1], ":")
		if !ok || !strings.EqualFold(p, hexPorta) {
			continue
		}
		if locale && strings.EqualFold(ind, locale4) {
			inode = append(inode, c[9])
		} else {
			altri++
		}
	}
	return inode, altri, sc.Err()
}

// socketDelGruppo: gli inode dei socket aperti dai processi del gruppo (fd che puntano a
// «socket:[inode]»). Un processo che sparisce durante la lettura si salta.
func socketDelGruppo(gruppo int) map[string]bool {
	suoi := map[string]bool{}
	voci, err := os.ReadDir(radiceProc)
	if err != nil {
		return suoi
	}
	for _, v := range voci {
		pid, err := strconv.Atoi(v.Name())
		if err != nil {
			continue
		}
		if _, g, ok := statDi(pid); !ok || g != gruppo {
			continue
		}
		dir := radiceProc + "/" + v.Name() + "/fd"
		fd, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, d := range fd {
			l, err := os.Readlink(dir + "/" + d.Name())
			if i, ok := strings.CutPrefix(l, "socket:["); err == nil && ok {
				suoi[strings.TrimSuffix(i, "]")] = true
			}
		}
	}
	return suoi
}

// statDi legge stato e gruppo di processi di pid da /proc/<pid>/stat (dopo l'ultima «)», perché il
// nome del comando può contenere spazi e parentesi).
func statDi(pid int) (stato byte, gruppo int, ok bool) {
	b, err := os.ReadFile(radiceProc + "/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, 0, false
	}
	i := bytes.LastIndexByte(b, ')')
	if i < 0 {
		return 0, 0, false
	}
	c := strings.Fields(string(b[i+1:]))
	if len(c) < 3 || len(c[0]) != 1 {
		return 0, 0, false
	}
	g, err := strconv.Atoi(c[2])
	return c[0][0], g, err == nil
}
