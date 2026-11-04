package printing

import (
	"context"
	"fmt"
	"slices"
	"sync"
)

// Lavoro è un lavoro ricevuto dalla stampante finta.
type Lavoro struct {
	Coda    string
	PDF     []byte
	Opzioni Options
	ID      JobID
}

// Finta è la Printer dei test: code fisse, i lavori ricevuti in memoria, e gli errori scelti dal
// test (ErrElenco per List e Opzioni, ErrStampa per Print). Prima, se c'è, si chiama all'inizio di
// ogni Print (i test lo usano per fermare un lavoro a metà). Non lancia mai un comando.
type Finta struct {
	mu         sync.Mutex
	Code       []string
	Dichiarate map[string][]OpzioneCoda
	ErrElenco  error
	ErrStampa  error
	Prima      func()
	lavori     []Lavoro
}

// List dà le code della finta.
func (f *Finta) List(context.Context) ([]Queue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ErrElenco != nil {
		return nil, f.ErrElenco
	}
	qq := make([]Queue, len(f.Code))
	for i, c := range f.Code {
		qq[i] = Queue{Nome: c}
	}
	return qq, nil
}

// Print registra il lavoro come lo farebbe CUPS: la coda deve esistere, gli argomenti sono quelli
// di lp (stesse validazioni), l'id è «<coda>-<n>».
func (f *Finta) Print(ctx context.Context, queue string, pdf []byte, opt Options) (JobID, error) {
	f.mu.Lock()
	prima := f.Prima
	f.mu.Unlock()
	if prima != nil {
		prima()
	}
	if _, err := argomentiLp(queue, opt); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ErrStampa != nil {
		return "", f.ErrStampa
	}
	if !slices.Contains(f.Code, queue) {
		return "", Rifiuto{Motivo: "lp: The printer or class does not exist."}
	}
	id := JobID(fmt.Sprintf("%s-%d", queue, len(f.lavori)+1))
	f.lavori = append(f.lavori, Lavoro{Coda: queue, PDF: slices.Clone(pdf), Opzioni: opt, ID: id})
	return id, nil
}

// Opzioni dà le opzioni dichiarate per la coda (nessuna se non ci sono).
func (f *Finta) Opzioni(_ context.Context, queue string) ([]OpzioneCoda, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ErrElenco != nil {
		return nil, f.ErrElenco
	}
	return f.Dichiarate[queue], nil
}

// Lavori sono i lavori ricevuti, in ordine.
func (f *Finta) Lavori() []Lavoro {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.lavori)
}
