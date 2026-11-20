package telefono

import "testing"

// inventario: ART-02
// normalizza_telefono: un «+» in testa e le cifre che Python chiama cifre (anche ², ④, ٣, ፩),
// niente altro; senza cifre il telefono è vuoto.
func TestNormalizzaTelefono(t *testing.T) {
	for _, c := range [][2]string{
		{" +٣-² ④AⅫ¼ ", "+٣²④"},
		{"tel ፩/⁰.₉", "፩⁰₉"},
		{"+decorazioni", ""},
		{" 02 12 ", "0212"},
		{"", ""},
		{"   ", ""},
		{"3+33", "333"},
		{" +", ""},
		{"①⑨⑴⒐⓪⓿❶➀➊🄀", "①⑨⑴⒐⓪⓿❶➀➊🄀"},
		{"⑩⑽⒑⓫❿½", ""},
		{"𐩀𐩃𐹠𑁒𑁙፱", "𐩀𐩃𐹠𑁒𑁙፱"},
		{"௧੧", "௧੧"},
	} {
		if got := Normalizza(c[0]); got != c[1] {
			t.Errorf("Normalizza(%q) = %q, atteso %q", c[0], got, c[1])
		}
	}
}
