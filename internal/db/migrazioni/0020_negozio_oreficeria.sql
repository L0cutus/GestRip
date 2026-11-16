-- La riga 1 di oreficerie, «il negozio stesso» (slice 086, ORE-01): l'id 1 non è un'oreficeria cliente (le esterne
-- sono id > 1, Oreficeria.is_b2b nello standalone). Lo standalone la semina con la sua migrazione
-- 20260906213000 (`INSERT OR IGNORE INTO oreficerie(id, name) VALUES (1, 'Negozio')`): senza, su un database che non
-- viene da un import la prima oreficeria creata prenderebbe l'id 1 e non si vedrebbe più (la 0007 diceva «qui non
-- si semina»: lo faceva l'import). Stessa istruzione, stessi valori (nome «Negozio», nessun telefono).
-- OR IGNORE: se l'id 1 (o il nome) c'è già, per esempio dopo un import, la riga resta com'è. L'import, che copia la
-- riga 1 dello standalone con le altre, toglie prima questa (importer.Importa).
INSERT OR IGNORE INTO oreficerie (id, nome) VALUES (1, 'Negozio');
