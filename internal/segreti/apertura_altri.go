//go:build !unix

package segreti

// aperturaSicura: fuori da unix (solo sviluppo) bastano lstat e il confronto dopo l'apertura.
const aperturaSicura = 0
