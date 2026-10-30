package apparecchio

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// albero è un /proc e un /sys sintetici, con la disposizione del kernel: /sys/dev/block/M:m e
// /sys/class/block/<nome> sono link relativi alla cartella del dispositivo, e slaves/ ha un link
// per ogni dispositivo sotto. ids dà il montaggio (il mnt_id di fdinfo) di un file aperto, per
// percorso; quelli che non ci sono stanno sul montaggio 40.
type albero struct {
	t      *testing.T
	radice string
	ids    map[string]string
}

func nuovoAlbero(t *testing.T) *albero {
	return &albero{t: t, radice: t.TempDir(), ids: map[string]string{}}
}

func (a *albero) id(f *os.File) (string, error) {
	if id, ok := a.ids[f.Name()]; ok {
		return id, nil
	}
	return "40", nil
}

// controlla controlla il database gestrip.db nella cartella d.
func (a *albero) controlla(d string) error {
	return ControllaVolumeCifrato(Sistema{Radice: a.radice, IdMontaggio: a.id}, filepath.Join(d, "gestrip.db"))
}

func (a *albero) scrivi(rel, testo string) {
	a.t.Helper()
	p := filepath.Join(a.radice, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		a.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(testo), 0o644); err != nil {
		a.t.Fatal(err)
	}
}

func (a *albero) link(rel, dest string) {
	a.t.Helper()
	p := filepath.Join(a.radice, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		a.t.Fatal(err)
	}
	if err := os.Symlink(dest, p); err != nil {
		a.t.Fatal(err)
	}
}

// dispositivo aggiunge un dispositivo a blocchi. Con uuid vuoto non è un device-mapper (un
// disco); sotto sono i nomi dei dispositivi in slaves/.
func (a *albero) dispositivo(nome, majmin, dmNome, uuid string, sotto ...string) {
	a.t.Helper()
	dir := "sys/devices/virtual/block/" + nome
	a.scrivi(dir+"/dev", majmin+"\n")
	if uuid != "" {
		a.scrivi(dir+"/dm/uuid", uuid+"\n")
		a.scrivi(dir+"/dm/name", dmNome+"\n")
	}
	if err := os.MkdirAll(filepath.Join(a.radice, dir, "slaves"), 0o755); err != nil {
		a.t.Fatal(err)
	}
	for _, s := range sotto {
		a.link(dir+"/slaves/"+s, "../../"+s)
	}
	a.link("sys/dev/block/"+majmin, "../../devices/virtual/block/"+nome)
	a.link("sys/class/block/"+nome, "../../devices/virtual/block/"+nome)
}

// partizione aggiunge una partizione del disco padre: sta dentro la sua cartella in /sys, con il
// file «partition», e non ha slaves/.
func (a *albero) partizione(padre, nome, majmin string) {
	a.t.Helper()
	dir := "sys/devices/virtual/block/" + padre + "/" + nome
	a.scrivi(dir+"/dev", majmin+"\n")
	a.scrivi(dir+"/partition", "1\n")
	if err := os.MkdirAll(filepath.Join(a.radice, dir, "slaves"), 0o755); err != nil {
		a.t.Fatal(err)
	}
	a.link("sys/dev/block/"+majmin, "../../devices/virtual/block/"+padre+"/"+nome)
	a.link("sys/class/block/"+nome, "../../devices/virtual/block/"+padre+"/"+nome)
}

// btrfs dichiara i dispositivi del filesystem btrfs fsid, come /sys/fs/btrfs/<fsid>/devices/.
func (a *albero) btrfs(fsid string, nomi ...string) {
	a.t.Helper()
	for _, n := range nomi {
		dest := "../../../../devices/virtual/block/" + n
		if r, err := filepath.EvalSymlinks(filepath.Join(a.radice, "sys/class/block", n)); err == nil {
			rel, _ := filepath.Rel(filepath.Join(a.radice, "sys/fs/btrfs", fsid, "devices"), r)
			dest = rel
		}
		a.link("sys/fs/btrfs/"+fsid+"/devices/"+n, dest)
	}
}

// monta scrive /proc/self/mountinfo: «/» (montaggio 1) su una partizione in chiaro e poi le righe
// date.
func (a *albero) monta(righe ...string) {
	a.t.Helper()
	a.scrivi("proc/self/mountinfo", strings.Join(append([]string{
		"1 0 259:2 / / rw,relatime shared:1 - ext4 /dev/nvme0n1p2 rw",
		"22 1 0:21 / /proc rw,nosuid shared:12 - proc proc rw",
	}, righe...), "\n")+"\n")
}

// dati è la cartella dei dati vera (i test la leggono come percorso reale, senza link).
func dati(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func riga(id, majmin, punto, tipo, sorgente string) string {
	return id + " 1 " + majmin + " / " + punto + " rw,relatime shared:5 - " + tipo + " " + sorgente + " rw"
}

func tocca(t *testing.T, p string) {
	t.Helper()
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

const (
	uuidLUKS2 = "CRYPT-LUKS2-0123456789abcdef0123456789abcdef-luks-dati"
	uuidLUKS1 = "CRYPT-LUKS1-0123456789abcdef0123456789abcdef-luks-vecchio"
	uuidLVM   = "LVM-AbCdEfGhIjKlMnOpQrStUvWxYz0123456789AbCdEfGhIjKlMnOpQrStUv"
	uuidMD    = "md-sintetico"
)

// inventario: OPS-12
// I dati sono su un volume cifrato: LUKS2 diretto, LVM sopra LUKS, partizione di un RAID md sopra
// LUKS, btrfs (dispositivo 0:N) sopra LUKS trovato dal nome in /dev/mapper, in /dev o da un link
// di /dev/disk, btrfs su due dischi tutti e due LUKS, LUKS1.
func TestVolumeCifratoAccettato(t *testing.T) {
	t.Run("ext4 su LUKS2", func(t *testing.T) {
		a, d := nuovoAlbero(t), dati(t)
		a.dispositivo("nvme0n1", "259:0", "", "")
		a.dispositivo("dm-0", "253:0", "luks-dati", uuidLUKS2, "nvme0n1")
		a.monta(riga("40", "253:0", d, "ext4", "/dev/mapper/luks-dati"))
		if err := a.controlla(d); err != nil {
			t.Error(err)
		}
		// Il database e i suoi file accanto, sullo stesso montaggio della cartella.
		for _, f := range []string{"gestrip.db", "gestrip.db-wal", "gestrip.db-shm"} {
			tocca(t, filepath.Join(d, f))
		}
		if err := a.controlla(d); err != nil {
			t.Errorf("con il database: %v", err)
		}
	})
	t.Run("LVM su LUKS2", func(t *testing.T) {
		a, d := nuovoAlbero(t), dati(t)
		a.dispositivo("sda", "8:0", "", "")
		a.dispositivo("dm-0", "253:0", "luks-pv", uuidLUKS2, "sda")
		a.dispositivo("dm-1", "253:1", "vg-dati", uuidLVM, "dm-0")
		a.monta(riga("40", "253:1", d, "ext4", "/dev/mapper/vg-dati"))
		if err := a.controlla(d); err != nil {
			t.Error(err)
		}
	})
	t.Run("partizione di un RAID md sopra due LUKS", func(t *testing.T) {
		a, d := nuovoAlbero(t), dati(t)
		a.dispositivo("dm-0", "253:0", "luks-a", uuidLUKS2)
		a.dispositivo("dm-1", "253:1", "luks-b", uuidLUKS2)
		a.dispositivo("md0", "9:0", "", "", "dm-0", "dm-1")
		a.partizione("md0", "md0p1", "259:9")
		a.monta(riga("40", "259:9", d, "ext4", "/dev/md0p1"))
		if err := a.controlla(d); err != nil {
			t.Error(err)
		}
	})
	t.Run("btrfs su LUKS2, sorgente in /dev/mapper", func(t *testing.T) {
		a, d := nuovoAlbero(t), dati(t)
		a.dispositivo("dm-0", "253:0", "luks-dati", uuidLUKS2)
		a.btrfs("fsid-a", "dm-0")
		a.monta(riga("40", "0:31", d, "btrfs", "/dev/mapper/luks-dati"))
		if err := a.controlla(d); err != nil {
			t.Error(err)
		}
	})
	t.Run("btrfs su LUKS2, sorgente /dev/dm-0", func(t *testing.T) {
		a, d := nuovoAlbero(t), dati(t)
		a.dispositivo("dm-0", "253:0", "luks-dati", uuidLUKS2)
		a.btrfs("fsid-a", "dm-0")
		a.monta(riga("40", "0:31", d, "btrfs", "/dev/dm-0"))
		if err := a.controlla(d); err != nil {
			t.Error(err)
		}
	})
	t.Run("btrfs su LUKS2, sorgente /dev/disk/by-uuid", func(t *testing.T) {
		a, d := nuovoAlbero(t), dati(t)
		a.dispositivo("dm-0", "253:0", "luks-dati", uuidLUKS2)
		a.btrfs("fsid-a", "dm-0")
		a.scrivi("dev/dm-0", "") // al posto del nodo del dispositivo: conta il nome
		a.link("dev/disk/by-uuid/fabfa8a2", "../../dm-0")
		a.monta(riga("40", "0:31", d, "btrfs", "/dev/disk/by-uuid/fabfa8a2"))
		if err := a.controlla(d); err != nil {
			t.Error(err)
		}
	})
	t.Run("btrfs su due dischi LUKS", func(t *testing.T) {
		a, d := nuovoAlbero(t), dati(t)
		a.dispositivo("dm-0", "253:0", "luks-a", uuidLUKS2)
		a.dispositivo("dm-1", "253:1", "luks-b", uuidLUKS2)
		a.btrfs("fsid-a", "dm-0", "dm-1")
		a.monta(riga("40", "0:31", d, "btrfs", "/dev/mapper/luks-a"))
		if err := a.controlla(d); err != nil {
			t.Error(err)
		}
	})
	t.Run("LUKS1", func(t *testing.T) {
		a, d := nuovoAlbero(t), dati(t)
		a.dispositivo("dm-0", "253:0", "luks-vecchio", uuidLUKS1)
		a.monta(riga("40", "253:0", d, "ext4", "/dev/mapper/luks-vecchio"))
		if err := a.controlla(d); err != nil {
			t.Error(err)
		}
	})
	t.Run("punto di montaggio con uno spazio", func(t *testing.T) {
		a, d := nuovoAlbero(t), dati(t)
		dir := filepath.Join(d, "con spazio")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		a.dispositivo("dm-0", "253:0", "luks-dati", uuidLUKS2)
		a.monta(riga("40", "253:0", strings.ReplaceAll(dir, " ", `\040`), "ext4", "/dev/mapper/luks-dati"))
		if err := a.controlla(dir); err != nil {
			t.Error(err)
		}
	})
}

// inventario: OPS-12
// Senza cifratura sotto i dati gestrip non parte: disco in chiaro, partizione di un disco in
// chiaro, LVM in chiaro, una sola gamba cifrata su due, dm-crypt senza LUKS, tmpfs e overlay,
// btrfs in chiaro o con un disco in chiaro, montaggio che non si trova.
func TestVolumeInChiaroRifiutato(t *testing.T) {
	casi := []struct {
		nome    string
		prepara func(a *albero, d string)
	}{
		{"disco in chiaro", func(a *albero, d string) {
			a.dispositivo("sda", "8:0", "", "")
			a.monta(riga("40", "8:0", d, "ext4", "/dev/sda"))
		}},
		{"partizione di un disco in chiaro", func(a *albero, d string) {
			a.dispositivo("sda", "8:0", "", "")
			a.partizione("sda", "sda2", "8:2")
			a.monta(riga("40", "8:2", d, "ext4", "/dev/sda2"))
		}},
		{"partizione di un RAID md con una gamba in chiaro", func(a *albero, d string) {
			a.dispositivo("dm-0", "253:0", "luks-a", uuidLUKS2)
			a.dispositivo("sdb", "8:16", "", "")
			a.dispositivo("md0", "9:0", "", "", "dm-0", "sdb")
			a.partizione("md0", "md0p1", "259:9")
			a.monta(riga("40", "259:9", d, "ext4", "/dev/md0p1"))
		}},
		{"LVM su disco in chiaro", func(a *albero, d string) {
			a.dispositivo("sda", "8:0", "", "")
			a.dispositivo("dm-1", "253:1", "vg-dati", uuidLVM, "sda")
			a.monta(riga("40", "253:1", d, "ext4", "/dev/mapper/vg-dati"))
		}},
		{"LVM con una gamba cifrata e una no", func(a *albero, d string) {
			a.dispositivo("sda", "8:0", "", "")
			a.dispositivo("sdb", "8:16", "", "")
			a.dispositivo("dm-0", "253:0", "luks-a", uuidLUKS2, "sda")
			a.dispositivo("dm-1", "253:1", "vg-dati", uuidLVM, "dm-0", "sdb")
			a.monta(riga("40", "253:1", d, "ext4", "/dev/mapper/vg-dati"))
		}},
		{"dm-crypt senza LUKS", func(a *albero, d string) {
			a.dispositivo("dm-0", "253:0", "semplice", "CRYPT-PLAIN-semplice")
			a.monta(riga("40", "253:0", d, "ext4", "/dev/mapper/semplice"))
		}},
		{"nome che contiene LUKS ma non comincia così", func(a *albero, d string) {
			a.dispositivo("dm-0", "253:0", "finto", "LVM-CRYPT-LUKS2-finto")
			a.monta(riga("40", "253:0", d, "ext4", "/dev/mapper/finto"))
		}},
		{"tmpfs", func(a *albero, d string) {
			a.monta(riga("40", "0:45", d, "tmpfs", "tmpfs"))
		}},
		{"overlay (Docker)", func(a *albero, d string) {
			a.monta(riga("40", "0:46", d, "overlay", "overlay"))
		}},
		{"btrfs su partizione in chiaro", func(a *albero, d string) {
			a.dispositivo("nvme1n1", "259:6", "", "")
			a.partizione("nvme1n1", "nvme1n1p2", "259:7")
			a.btrfs("fsid-a", "nvme1n1p2")
			a.monta(riga("40", "0:30", d, "btrfs", "/dev/nvme1n1p2"))
		}},
		{"btrfs su due dischi, la sorgente LUKS e l'altro in chiaro", func(a *albero, d string) {
			a.dispositivo("dm-0", "253:0", "luks-a", uuidLUKS2)
			a.dispositivo("nvme2n1", "259:8", "", "")
			a.btrfs("fsid-a", "dm-0", "nvme2n1")
			a.monta(riga("40", "0:31", d, "btrfs", "/dev/mapper/luks-a"))
		}},
		{"btrfs senza /sys/fs/btrfs", func(a *albero, d string) {
			a.dispositivo("dm-0", "253:0", "luks-a", uuidLUKS2)
			a.monta(riga("40", "0:31", d, "btrfs", "/dev/mapper/luks-a"))
		}},
		{"btrfs su un nome di /dev/mapper che non c'è", func(a *albero, d string) {
			a.monta(riga("40", "0:30", d, "btrfs", "/dev/mapper/sparito"))
		}},
		{"dispositivo che manca in /sys", func(a *albero, d string) {
			a.monta(riga("40", "253:9", d, "ext4", "/dev/mapper/sparito"))
		}},
		{"slaves in giro tondo", func(a *albero, d string) {
			a.dispositivo("dm-0", "253:0", "a", uuidLVM, "dm-1")
			a.dispositivo("dm-1", "253:1", "b", uuidLVM, "dm-0")
			a.monta(riga("40", "253:1", d, "ext4", "/dev/mapper/b"))
		}},
		{"tmpfs montato dopo sulla cartella madre", func(a *albero, d string) {
			// La cartella è stata montata da LUKS (40), poi un tmpfs (41) ha coperto la madre: il
			// percorso porta al tmpfs, e fdinfo lo dice; il punto più lungo direbbe LUKS.
			a.dispositivo("dm-0", "253:0", "luks-dati", uuidLUKS2)
			a.monta(riga("40", "253:0", d, "ext4", "/dev/mapper/luks-dati"),
				riga("41", "0:50", filepath.Dir(d), "tmpfs", "tmpfs"))
			a.ids[d] = "41"
		}},
		{"montaggio in chiaro sopra uno cifrato sullo stesso punto", func(a *albero, d string) {
			a.dispositivo("dm-0", "253:0", "luks-dati", uuidLUKS2)
			a.dispositivo("sda", "8:0", "", "")
			a.monta(riga("40", "253:0", d, "ext4", "/dev/mapper/luks-dati"),
				riga("41", "8:0", d, "ext4", "/dev/sda"))
			a.ids[d] = "41"
		}},
		{"id del montaggio assente in mountinfo", func(a *albero, d string) {
			a.dispositivo("dm-0", "253:0", "luks-dati", uuidLUKS2)
			a.monta(riga("40", "253:0", d, "ext4", "/dev/mapper/luks-dati"))
			a.ids[d] = "77"
		}},
		{"mountinfo che manca", func(a *albero, d string) {}},
		{"mountinfo rovinato", func(a *albero, d string) {
			a.scrivi("proc/self/mountinfo", "riga senza campi\n")
		}},
	}
	for _, c := range casi {
		t.Run(c.nome, func(t *testing.T) {
			a, d := nuovoAlbero(t), dati(t)
			c.prepara(a, d)
			err := a.controlla(d)
			if err == nil || !strings.Contains(err.Error(), "volume cifrato") {
				t.Errorf("accettato o messaggio senza «volume cifrato»: %v", err)
			}
		})
	}
}

// inventario: OPS-12
// Il database, e il -wal e lo -shm che ci sono, stanno sul montaggio della cartella cifrata: un
// link (anche pendente) verso un'altra cartella o un file montato a parte (bind mount del solo
// file) è rifiutato. Nello standalone un database link era accettato.
func TestVolumeCifratoIlDatabaseStaNellaCartella(t *testing.T) {
	prepara := func(t *testing.T) (*albero, string) {
		a, d := nuovoAlbero(t), dati(t)
		a.dispositivo("dm-0", "253:0", "luks-dati", uuidLUKS2)
		a.dispositivo("sda", "8:0", "", "")
		a.monta(riga("40", "253:0", d, "ext4", "/dev/mapper/luks-dati"),
			riga("41", "8:0", filepath.Join(d, "gestrip.db"), "ext4", "/dev/sda"))
		return a, d
	}
	altrove := filepath.Join(dati(t), "gestrip.db")
	tocca(t, altrove)
	for _, f := range []string{"gestrip.db", "gestrip.db-wal", "gestrip.db-shm"} {
		t.Run(f+" link", func(t *testing.T) {
			a, d := prepara(t)
			if err := os.Symlink(altrove, filepath.Join(d, f)); err != nil {
				t.Fatal(err)
			}
			if err := a.controlla(d); err == nil || !strings.Contains(err.Error(), "è un link") {
				t.Errorf("link accettato: %v", err)
			}
		})
		t.Run(f+" su un altro montaggio", func(t *testing.T) {
			a, d := prepara(t)
			p := filepath.Join(d, f)
			tocca(t, p)
			a.ids[p] = "41"
			if err := a.controlla(d); err == nil || !strings.Contains(err.Error(), "altro montaggio") {
				t.Errorf("file su un altro montaggio accettato: %v", err)
			}
		})
	}
	t.Run("link pendente", func(t *testing.T) {
		a, d := prepara(t)
		if err := os.Symlink(filepath.Join(t.TempDir(), "non-ancora.db"), filepath.Join(d, "gestrip.db")); err != nil {
			t.Fatal(err)
		}
		if err := a.controlla(d); err == nil || !strings.Contains(err.Error(), "è un link") {
			t.Errorf("link pendente accettato: %v", err)
		}
	})
	t.Run("non un file regolare", func(t *testing.T) {
		a, d := prepara(t)
		if err := os.Mkdir(filepath.Join(d, "gestrip.db"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := a.controlla(d); err == nil || !strings.Contains(err.Error(), "file regolare") {
			t.Errorf("cartella al posto del database accettata: %v", err)
		}
	})
}

// inventario: OPS-12
// Il messaggio dice quale cartella e perché, ma non come si spegne il controllo: l'opzione da
// sviluppo e banco non si nomina (decisione di Stefano, 30/10/2026). Una cartella dei dati che non
// c'è si dice per quello che è.
func TestVolumeInChiaroMessaggio(t *testing.T) {
	a, d := nuovoAlbero(t), dati(t)
	a.dispositivo("sda", "8:0", "", "")
	a.monta(riga("40", "8:0", d, "ext4", "/dev/sda"))
	err := a.controlla(d)
	if err == nil {
		t.Fatal("accettato")
	}
	for _, parte := range []string{d, "LUKS", "/dev/sda"} {
		if !strings.Contains(err.Error(), parte) {
			t.Errorf("manca %q in %q", parte, err)
		}
	}
	if strings.Contains(err.Error(), "senza-volume-cifrato") || strings.Contains(err.Error(), "sviluppo") {
		t.Errorf("il rifiuto dice come spegnere il controllo: %q", err)
	}
	manca := filepath.Join(d, "manca")
	err = a.controlla(manca)
	if err == nil || !strings.Contains(err.Error(), "la cartella dei dati "+manca+" non esiste") || strings.Contains(err.Error(), "volume cifrato") {
		t.Errorf("cartella che manca: %v", err)
	}
}

// catena prepara n device-mapper uno sopra l'altro (dm-0 in fondo, LUKS2) e monta la cartella d
// da quello in cima.
func catena(a *albero, d string, n int) {
	a.dispositivo("dm-0", "253:0", "luks-fondo", uuidLUKS2)
	for i := 1; i < n; i++ {
		a.dispositivo(fmt.Sprintf("dm-%d", i), fmt.Sprintf("253:%d", i), fmt.Sprintf("lv-%d", i), uuidLVM, fmt.Sprintf("dm-%d", i-1))
	}
	a.monta(riga("40", fmt.Sprintf("253:%d", n-1), d, "ext4", fmt.Sprintf("/dev/mapper/lv-%d", n-1)))
}

// inventario: OPS-12
// La discesa in slaves/ ha un limite: 17 device-mapper con LUKS in fondo vanno bene, 18 no
// (troppi livelli), anche senza giro tondo.
func TestVolumeCatenaDiDispositivi(t *testing.T) {
	a, d := nuovoAlbero(t), dati(t)
	catena(a, d, 17)
	if err := a.controlla(d); err != nil {
		t.Errorf("17 livelli: %v", err)
	}
	a, d = nuovoAlbero(t), dati(t)
	catena(a, d, 18)
	if err := a.controlla(d); err == nil || !strings.Contains(err.Error(), "troppi livelli") {
		t.Errorf("18 livelli: %v", err)
	}
}

// inventario: OPS-12
// La cartella dei dati può essere un link a una cartella sul volume cifrato: conta quella vera.
func TestVolumeCartellaLinkSuLUKS(t *testing.T) {
	a, d := nuovoAlbero(t), dati(t)
	a.dispositivo("dm-0", "253:0", "luks-dati", uuidLUKS2)
	a.monta(riga("40", "253:0", d, "ext4", "/dev/mapper/luks-dati"))
	link := filepath.Join(t.TempDir(), "dati")
	if err := os.Symlink(d, link); err != nil {
		t.Fatal(err)
	}
	if err := a.controlla(link); err != nil {
		t.Error(err)
	}
}

// inventario: OPS-12
// La riga del montaggio è quella con l'id esatto, non un'altra che lo contiene (400 non è 40); una
// riga del montaggio senza « - » è rovinata, anche se il dispositivo è LUKS.
func TestVolumeRigaDelMontaggio(t *testing.T) {
	a, d := nuovoAlbero(t), dati(t)
	a.dispositivo("dm-0", "253:0", "luks-dati", uuidLUKS2)
	a.dispositivo("sda", "8:0", "", "")
	a.monta(riga("400", "253:0", d, "ext4", "/dev/mapper/luks-dati"), riga("40", "8:0", d, "ext4", "/dev/sda"))
	if err := a.controlla(d); err == nil || !strings.Contains(err.Error(), "sda non è cifrato") {
		t.Errorf("id 40 dopo 400: %v", err)
	}
	a, d = nuovoAlbero(t), dati(t)
	a.dispositivo("dm-0", "253:0", "luks-dati", uuidLUKS2)
	a.monta("40 1 253:0 / " + d + " rw,relatime shared:5 ext4 /dev/mapper/luks-dati rw")
	if err := a.controlla(d); err == nil || !strings.Contains(err.Error(), "forma sconosciuta") {
		t.Errorf("riga senza « - »: %v", err)
	}
}

// inventario: OPS-12
// Gli escape ottali del kernel nei percorsi di mountinfo: solo tre cifre ottali intere.
func TestSenzaEscape(t *testing.T) {
	for in, atteso := range map[string]string{
		`a\040b`: "a b", `a\134b`: `a\b`, `x\040`: "x ", `\9xy`: `\9xy`, `\04`: `\04`, `senza`: "senza", `a\011b\012`: "a\tb\n",
	} {
		if got := senzaEscape(in); got != atteso {
			t.Errorf("%q: %q, atteso %q", in, got, atteso)
		}
	}
	// Una sorgente btrfs in /dev/mapper con uno spazio nel nome.
	a, d := nuovoAlbero(t), dati(t)
	a.dispositivo("dm-0", "253:0", "luks dati", uuidLUKS2)
	a.btrfs("fsid-a", "dm-0")
	a.monta(riga("40", "0:31", d, "btrfs", `/dev/mapper/luks\040dati`))
	if err := a.controlla(d); err != nil {
		t.Errorf("sorgente con uno spazio: %v", err)
	}
}

// inventario: OPS-12
// Un dispositivo 0:N vale solo per btrfs con una sorgente in /dev, e la sorgente decide quale
// filesystem e quali dischi: zfs non è un disco, «dm-0» senza /dev/ no, e la sorgente in chiaro
// (o un nome di /dev/mapper in chiaro) non prende i dischi cifrati di un altro btrfs.
func TestVolumeBtrfsSorgente(t *testing.T) {
	casi := []struct {
		nome, attesa string
		prepara      func(a *albero, d string)
	}{
		{"zfs", "non è un disco", func(a *albero, d string) {
			a.dispositivo("dm-0", "253:0", "luks-dati", uuidLUKS2)
			a.btrfs("fsid-a", "dm-0")
			a.monta(riga("40", "0:31", d, "zfs", "/dev/mapper/luks-dati"))
		}},
		{"sorgente senza /dev/", "non è un disco", func(a *albero, d string) {
			a.dispositivo("dm-0", "253:0", "luks-dati", uuidLUKS2)
			a.btrfs("fsid-a", "dm-0")
			a.monta(riga("40", "0:31", d, "btrfs", "dm-0"))
		}},
		{"sorgente in chiaro in un altro btrfs", "sda non è cifrato", func(a *albero, d string) {
			a.dispositivo("dm-0", "253:0", "luks-dati", uuidLUKS2)
			a.dispositivo("sda", "8:0", "", "")
			a.btrfs("fsid-a", "dm-0")
			a.btrfs("fsid-b", "sda")
			a.monta(riga("40", "0:31", d, "btrfs", "/dev/sda"))
		}},
		{"nome di /dev/mapper in chiaro in un altro btrfs", "dm-1 non è cifrato", func(a *albero, d string) {
			a.dispositivo("dm-0", "253:0", "dati2", uuidLUKS2)
			a.dispositivo("dm-1", "253:1", "dati", uuidLVM)
			a.btrfs("fsid-a", "dm-0")
			a.btrfs("fsid-b", "dm-1")
			a.monta(riga("40", "0:31", d, "btrfs", "/dev/mapper/dati"))
		}},
	}
	for _, c := range casi {
		t.Run(c.nome, func(t *testing.T) {
			a, d := nuovoAlbero(t), dati(t)
			c.prepara(a, d)
			if err := a.controlla(d); err == nil || !strings.Contains(err.Error(), c.attesa) {
				t.Errorf("atteso «%s»: %v", c.attesa, err)
			}
		})
	}
}

// inventario: OPS-12
// Anche salire da una partizione al suo disco conta come un livello: 16 device-mapper con una
// partizione in cima vanno bene, 17 no.
func TestVolumeCatenaConPartizione(t *testing.T) {
	prepara := func(n int) (*albero, string) {
		a, d := nuovoAlbero(t), dati(t)
		a.dispositivo("dm-0", "253:0", "luks-fondo", uuidLUKS2)
		for i := 1; i < n; i++ {
			a.dispositivo(fmt.Sprintf("dm-%d", i), fmt.Sprintf("253:%d", i), fmt.Sprintf("lv-%d", i), uuidLVM, fmt.Sprintf("dm-%d", i-1))
		}
		cima := fmt.Sprintf("dm-%d", n-1)
		a.partizione(cima, cima+"p1", "259:99")
		a.monta(riga("40", "259:99", d, "ext4", "/dev/"+cima+"p1"))
		return a, d
	}
	if a, d := prepara(16); a.controlla(d) != nil {
		t.Errorf("16 livelli più la partizione: %v", a.controlla(d))
	}
	if a, d := prepara(17); a.controlla(d) == nil || !strings.Contains(a.controlla(d).Error(), "troppi livelli") {
		t.Errorf("17 livelli più la partizione: %v", a.controlla(d))
	}
}

// inventario: OPS-12
// Il messaggio di rifiuto dice il punto di montaggio com'è, senza gli escape del kernel.
func TestVolumeMessaggioPuntoConSpazio(t *testing.T) {
	a, d := nuovoAlbero(t), dati(t)
	dir := filepath.Join(d, "con spazio")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	a.dispositivo("sda", "8:0", "", "")
	a.monta(riga("40", "8:0", strings.ReplaceAll(dir, " ", `\040`), "ext4", "/dev/sda"))
	if err := a.controlla(dir); err == nil || !strings.Contains(err.Error(), "montata in "+dir+" da /dev/sda") {
		t.Errorf("messaggio: %v", err)
	}
}
