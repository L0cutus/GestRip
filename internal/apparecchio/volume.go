package apparecchio

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// prefissoLUKS comincia l'uuid device-mapper di un volume aperto da cryptsetup con LUKS
// (CRYPT-LUKS2-…, o CRYPT-LUKS1-…). dm-crypt senza LUKS (CRYPT-PLAIN-…) non basta.
const prefissoLUKS = "CRYPT-LUKS"

// livelliMassimi limita la discesa in slaves/ (LVM su RAID su LUKS…): oltre è un giro tondo.
const livelliMassimi = 16

// OpzioneSenzaVolumeCifrato è l'opzione di «gestrip serve» che salta il controllo.
const OpzioneSenzaVolumeCifrato = "-senza-volume-cifrato"

// Sistema dice dove il controllo del volume legge /proc/self/mountinfo e /sys (Radice, «/»
// sull'apparecchio) e come sa su quale montaggio sta un file aperto (IdMontaggio; nil:
// IdMontaggioDaFdinfo). I test ci mettono un sistema sintetico.
type Sistema struct {
	Radice      string
	IdMontaggio func(f *os.File) (string, error)
}

// montaggio è una riga di /proc/self/mountinfo.
type montaggio struct {
	id, punto, dispositivo, tipo, sorgente string
}

// IdMontaggioDaFdinfo dà il numero del montaggio di un file aperto, la riga «mnt_id:» di
// /proc/self/fdinfo/<fd> (Linux 3.15 e dopo): è il primo campo della sua riga in mountinfo. Va
// dal descrittore, non dal percorso, quindi un montaggio che copre la cartella madre dopo, un link
// o un bind mount del solo file non ingannano.
func IdMontaggioDaFdinfo(f *os.File) (string, error) {
	b, err := os.ReadFile("/proc/self/fdinfo/" + strconv.FormatUint(uint64(f.Fd()), 10))
	if err != nil {
		return "", fmt.Errorf("/proc/self/fdinfo non si legge (%v)", errors.Unwrap(err))
	}
	for r := range strings.SplitSeq(string(b), "\n") {
		if v, ok := strings.CutPrefix(r, "mnt_id:"); ok {
			v = strings.TrimSpace(v)
			if _, err := strconv.ParseUint(v, 10, 64); err != nil {
				break
			}
			return v, nil
		}
	}
	return "", errors.New("mnt_id assente in /proc/self/fdinfo")
}

// ControllaVolumeCifrato controlla che il database percorsoDB stia su un volume cifrato con LUKS
// (OPS-12: il Go non ha SQLCipher, i dati stanno in chiaro dentro il disco cifrato, quindi senza
// il disco cifrato non si parte):
//   - la cartella del database c'è; si apre senza seguire un link e se ne legge il montaggio;
//   - il database, il -wal e lo -shm che ci sono già non sono link e stanno sullo stesso
//     montaggio (un bind mount del solo file è rifiutato); quelli che mancano li crea SQLite lì;
//   - il dispositivo a blocchi del montaggio (per btrfs, che ha un dispositivo 0:N, tutti i suoi
//     dischi) arriva a un device-mapper LUKS per ogni ramo di slaves/; una partizione vale come il
//     suo disco (LVM sopra LUKS va bene, LVM o RAID con una gamba in chiaro no).
//
// Ogni dubbio è un rifiuto.
func ControllaVolumeCifrato(s Sistema, percorsoDB string) error {
	assoluto, err := filepath.Abs(percorsoDB)
	if err != nil {
		return err
	}
	dir, nome := filepath.Dir(assoluto), filepath.Base(assoluto)
	rifiuto := func(motivo string, a ...any) error {
		return fmt.Errorf("la cartella dei dati %s non sta su un volume cifrato (LUKS): %s; gestrip non parte "+
			"(solo per lo sviluppo e il banco, mai sull'apparecchio: gestrip serve %s)", dir, fmt.Sprintf(motivo, a...), OpzioneSenzaVolumeCifrato)
	}
	reale, err := filepath.EvalSymlinks(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("la cartella dei dati %s non esiste: gestrip non parte (la crea l'installer, dentro il disco cifrato)", dir)
	}
	if err != nil {
		return rifiuto("la cartella non si legge (%v)", errors.Unwrap(err))
	}
	idMontaggio := s.IdMontaggio
	if idMontaggio == nil {
		idMontaggio = IdMontaggioDaFdinfo
	}
	cartella, err := os.OpenFile(reale, os.O_RDONLY|aperturaCartella, 0)
	if err != nil {
		return rifiuto("la cartella non si apre (%v)", errors.Unwrap(err))
	}
	id, err := idMontaggio(cartella)
	_ = cartella.Close() // in sola lettura: la chiusura non perde niente
	if err != nil {
		return rifiuto("%v", err)
	}
	for _, f := range []string{nome, nome + "-wal", nome + "-shm"} {
		p := filepath.Join(reale, f)
		fi, err := os.Lstat(p)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			return rifiuto("%s non si legge (%v)", f, errors.Unwrap(err))
		case fi.Mode()&os.ModeSymlink != 0:
			return rifiuto("%s è un link: il database deve stare nella cartella dei dati", f)
		case !fi.Mode().IsRegular():
			return rifiuto("%s non è un file regolare", f)
		}
		g, err := os.OpenFile(p, os.O_RDONLY|aperturaFile, 0)
		if err != nil {
			return rifiuto("%s non si apre (%v)", f, errors.Unwrap(err))
		}
		idFile, err := idMontaggio(g)
		_ = g.Close() // in sola lettura
		if err != nil {
			return rifiuto("%v", err)
		}
		if idFile != id {
			return rifiuto("%s sta su un altro montaggio (%s) della cartella (%s)", f, idFile, id)
		}
	}
	m, err := montaggioConId(s.Radice, id)
	if err != nil {
		return rifiuto("%v", err)
	}
	dischi, err := dispositiviDi(s.Radice, m)
	if err != nil {
		return rifiuto("montata in %s da %s (%s): %v", m.punto, m.sorgente, m.tipo, err)
	}
	for _, d := range dischi {
		if motivo := cifrato(d, 0); motivo != "" {
			return rifiuto("montata in %s da %s (%s): %s", m.punto, m.sorgente, m.tipo, motivo)
		}
	}
	return nil
}

// montaggioConId dà la riga di /proc/self/mountinfo del montaggio id. Una riga rovinata è un
// errore.
func montaggioConId(radice, id string) (montaggio, error) {
	b, err := os.ReadFile(filepath.Join(radice, "proc/self/mountinfo"))
	if err != nil {
		return montaggio{}, fmt.Errorf("/proc/self/mountinfo non si legge (%v)", errors.Unwrap(err))
	}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for n := 1; sc.Scan(); n++ {
		campi := strings.Split(sc.Text(), " ")
		sep := -1
		for i := 6; i < len(campi); i++ {
			if campi[i] == "-" {
				sep = i
				break
			}
		}
		if sep < 0 || sep+2 >= len(campi) {
			return montaggio{}, fmt.Errorf("/proc/self/mountinfo, riga %d: forma sconosciuta", n)
		}
		if campi[0] == id {
			return montaggio{id: id, punto: senzaEscape(campi[4]), dispositivo: campi[2], tipo: campi[sep+1],
				sorgente: senzaEscape(campi[sep+2])}, nil
		}
	}
	if err := sc.Err(); err != nil {
		return montaggio{}, err
	}
	return montaggio{}, fmt.Errorf("montaggio %s assente in /proc/self/mountinfo", id)
}

// senzaEscape toglie gli escape ottali del kernel (\040 per lo spazio, \011, \012, \134).
func senzaEscape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+4 <= len(s) {
			if v, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// dispositiviDi dà le cartelle in /sys dei dispositivi a blocchi sotto il montaggio. Un
// dispositivo 0:N (btrfs, ma anche tmpfs, overlay, rete) non è un disco: per btrfs si trova la
// sorgente in /sys/class/block e poi tutti i dischi del suo filesystem in /sys/fs/btrfs.
func dispositiviDi(radice string, m montaggio) ([]string, error) {
	if !strings.HasPrefix(m.dispositivo, "0:") {
		d := filepath.Join(radice, "sys/dev/block", m.dispositivo)
		if _, err := os.Stat(d); err != nil {
			return nil, fmt.Errorf("dispositivo %s assente in /sys/dev/block", m.dispositivo)
		}
		return []string{d}, nil
	}
	if m.tipo != "btrfs" || !strings.HasPrefix(m.sorgente, "/dev/") {
		return nil, errors.New("non è un disco")
	}
	nome, err := nomeDellaSorgente(radice, m.sorgente)
	if err != nil {
		return nil, err
	}
	// Il filesystem btrfs che ha la sorgente tra i suoi dischi: contano tutti.
	cartelle, _ := filepath.Glob(filepath.Join(radice, "sys/fs/btrfs/*/devices"))
	for _, c := range cartelle {
		voci, err := os.ReadDir(c)
		if err != nil {
			continue
		}
		var dischi []string
		trovato := false
		for _, v := range voci {
			dischi = append(dischi, filepath.Join(c, v.Name()))
			trovato = trovato || v.Name() == nome
		}
		if trovato {
			return dischi, nil
		}
	}
	return nil, fmt.Errorf("i dischi del btrfs su %s non si trovano in /sys/fs/btrfs", nome)
}

// nomeDellaSorgente dà il nome del kernel (dm-0, nvme1n1p2…) della sorgente in /dev di un
// montaggio. I link sotto /dev (/dev/disk/by-uuid/…, /dev/mapper/…) si risolvono; se non ci sono,
// /dev/mapper/NOME si cerca dal dm/name.
func nomeDellaSorgente(radice, sorgente string) (string, error) {
	radiceReale, err := filepath.EvalSymlinks(radice)
	if err != nil {
		return "", err
	}
	devReale := filepath.Join(radiceReale, "dev")
	if r, err := filepath.EvalSymlinks(filepath.Join(radiceReale, sorgente)); err == nil {
		if rel, err := filepath.Rel(devReale, r); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			sorgente = "/dev/" + rel
		}
	}
	classe := filepath.Join(radice, "sys/class/block")
	if nome, ok := strings.CutPrefix(sorgente, "/dev/mapper/"); ok && !strings.Contains(nome, "/") {
		voci, err := os.ReadDir(classe)
		if err != nil {
			return "", errors.New("/sys/class/block non si legge")
		}
		for _, v := range voci {
			b, err := os.ReadFile(filepath.Join(classe, v.Name(), "dm/name"))
			if err == nil && strings.TrimSpace(string(b)) == nome {
				return v.Name(), nil
			}
		}
		return "", errors.New("dispositivo assente in /sys/class/block")
	}
	nome := strings.TrimPrefix(sorgente, "/dev/")
	if strings.Contains(nome, "/") || nome == "" || nome == "." || nome == ".." {
		return "", errors.New("sorgente non riconosciuta")
	}
	if _, err := os.Stat(filepath.Join(classe, nome)); err != nil {
		return "", errors.New("dispositivo assente in /sys/class/block")
	}
	return nome, nil
}

// cifrato dice perché il dispositivo in /sys d non è cifrato, o "" se lo è: è un device-mapper
// LUKS, è una partizione di un disco cifrato, oppure tutto quello che ha sotto (slaves/) lo è.
func cifrato(d string, livello int) string {
	nome := filepath.Base(d)
	reale, err := filepath.EvalSymlinks(d)
	if err == nil {
		nome = filepath.Base(reale)
	}
	if livello > livelliMassimi {
		return "troppi livelli di dispositivi sotto " + nome
	}
	if b, err := os.ReadFile(filepath.Join(d, "dm/uuid")); err == nil && strings.HasPrefix(strings.TrimSpace(string(b)), prefissoLUKS) {
		return ""
	}
	// Una partizione sta nella cartella del suo disco: vale quello che c'è sotto il disco.
	if _, e := os.Stat(filepath.Join(d, "partition")); e == nil && err == nil {
		return cifrato(filepath.Dir(reale), livello+1)
	}
	sotto, err := os.ReadDir(filepath.Join(d, "slaves"))
	if err != nil || len(sotto) == 0 {
		return nome + " non è cifrato"
	}
	for _, s := range sotto {
		if motivo := cifrato(filepath.Join(d, "slaves", s.Name()), livello+1); motivo != "" {
			return motivo
		}
	}
	return ""
}
