//go:build !unix

package db

// aperturaPrivata fuori da unix (solo sviluppo): bastano i controlli con Lstat.
const aperturaPrivata = 0
