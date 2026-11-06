-- La ricerca della busta per codice QR (RIP-H-22, D-63, slice 052): la scansione dalla home confronta
-- il codice letto, senza spazi ai bordi, con codice_qr com'è salvato, uguale esatto. Il codice non è
-- unico (buste gemelle, RIP-H-22 deciso il 29/10/2026), quindi l'indice non è UNIQUE. Lo standalone
-- non ha l'indice (la colonna è del 20260909090000, senza indice): solo del Go, in IndiciSoloGo.
CREATE INDEX buste_codice_qr ON buste (codice_qr);
