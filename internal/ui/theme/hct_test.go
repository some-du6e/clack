package theme

import (
	"fmt"
	"image/color"
	"math"
	"strconv"
	"testing"
)

// Golden vectors from Slack's own Material Color Utilities build (webpack
// module "t+RV", gantry-v2-vendors) run in node: generatePaletteFromHex for
// light and dark, getClosestCustomPaletteShadeFromCustomHex for each, and
// hexToHct(hex).tone. The first 18 are hand-picked (Slack preset custom hexes,
// greys, extremes, and #20C3F5, the sRGB value whose tone sits closest to a
// shade-choice boundary); the rest are pseudo-random.
var hctVectors = []struct {
	hex               string
	tone              float64
	keyLight, keyDark int
	light, dark       [12]string
}{
	{"4C64A6", 43.36579724536723, 6, 5,
		[12]string{"F6F6FF", "DEE4FF", "C7D3FF", "ABBFFF", "8FA6ED", "6F87CB", "5068AA", "3E5798", "253F7F", "0A2A6A", "001A4D", "000C2D"},
		[12]string{"000C2D", "001A4D", "0A2A6A", "253F7F", "3E5798", "5068AA", "6F87CB", "8FA6ED", "ABBFFF", "C7D3FF", "DEE4FF", "F6F6FF"}},
	{"9C0000", 31.961032508501233, 8, 3,
		[12]string{"FFF4F2", "FFDED9", "FFC7BE", "FFAC9F", "FF8573", "F64B38", "CB2C1E", "B2180D", "8B0000", "650000", "450000", "280000"},
		[12]string{"280000", "450000", "650000", "8B0000", "B2180D", "CB2C1E", "F64B38", "FF8573", "FFAC9F", "FFC7BE", "FFDED9", "FFF4F2"}},
	{"FAD961", 87.39396309828405, 2, 9,
		[12]string{"FFF6E2", "FFE488", "F3D25B", "DEBF4A", "C4A633", "A28710", "7F6900", "6B5800", "504100", "382D00", "251D00", "130E00"},
		[12]string{"130E00", "251D00", "382D00", "504100", "6B5800", "7F6900", "A28710", "C4A633", "DEBF4A", "F3D25B", "FFE488", "FFF6E2"}},
	{"543312", 24.627263764035824, 8, 3,
		[12]string{"FFF4EE", "FFE0C7", "FFCA9D", "EAB78B", "CF9F75", "AC8058", "8B623D", "78512E", "5D3A19", "462707", "301700", "1A0A00"},
		[12]string{"1A0A00", "301700", "462707", "5D3A19", "78512E", "8B623D", "AC8058", "CF9F75", "EAB78B", "FFCA9D", "FFE0C7", "FFF4EE"}},
	{"000000", 0, 11, 0,
		[12]string{"F6F6F6", "E5E5E5", "D4D4D4", "C1C1C1", "A8A8A8", "898989", "6A6A6A", "595959", "424242", "2E2E2E", "1D1D1D", "0E0E0E"},
		[12]string{"0E0E0E", "1D1D1D", "2E2E2E", "424242", "595959", "6A6A6A", "898989", "A8A8A8", "C1C1C1", "D4D4D4", "E5E5E5", "F6F6F6"}},
	{"F8F8F8", 97.57900529716919, 0, 11,
		[12]string{"F6F6F6", "E5E5E5", "D4D4D4", "C0C1C1", "A8A9A9", "888989", "696B6B", "585A5A", "414243", "2D2F2F", "1C1E1E", "0C0F0F"},
		[12]string{"0C0F0F", "1C1E1E", "2D2F2F", "414243", "585A5A", "696B6B", "888989", "A8A9A9", "C0C1C1", "D4D4D4", "E5E5E5", "F6F6F6"}},
	{"2D0033", 7.198411472284555, 11, 0,
		[12]string{"FFF3FA", "FFDAFB", "FFC0FD", "EAADE9", "D095CF", "AE76AE", "8D588E", "7A477C", "603063", "491B4D", "36073B", "200025"},
		[12]string{"200025", "36073B", "491B4D", "603063", "7A477C", "8D588E", "AE76AE", "D095CF", "EAADE9", "FFC0FD", "FFDAFB", "FFF3FA"}},
	{"EF3E75", 55.15497977988167, 5, 6,
		[12]string{"FFF4F4", "FFDDE2", "FFC6CF", "FFA9BA", "FF819E", "F5437A", "CC1F5C", "B3004C", "870038", "620026", "430018", "26000A"},
		[12]string{"26000A", "430018", "620026", "870038", "B3004C", "CC1F5C", "F5437A", "FF819E", "FFA9BA", "FFC6CF", "FFDDE2", "FFF4F4"}},
	{"FFFFFF", 100, 0, 11,
		[12]string{"F6F6F6", "E5E5E5", "D4D4D4", "C0C1C1", "A8A9A9", "888989", "696B6B", "585A5A", "414243", "2C2F2F", "1C1E1E", "0C0F0F"},
		[12]string{"0C0F0F", "1C1E1E", "2C2F2F", "414243", "585A5A", "696B6B", "888989", "A8A9A9", "C0C1C1", "D4D4D4", "E5E5E5", "F6F6F6"}},
	{"808080", 53.585013452169036, 5, 6,
		[12]string{"F8F6F6", "E6E5E5", "D5D4D4", "C1C1C1", "A9A8A8", "898989", "6A6A6B", "595A5A", "414242", "2D2E2E", "1D1E1E", "0D0E0F"},
		[12]string{"0D0E0F", "1D1E1E", "2D2E2E", "414242", "595A5A", "6A6A6B", "898989", "A9A8A8", "C1C1C1", "D5D4D4", "E6E5E5", "F8F6F6"}},
	{"3F0E40", 14.064766169745116, 10, 1,
		[12]string{"FFF3F9", "FFDBF8", "FFC1F7", "EEACE7", "D394CD", "B175AC", "8F578C", "7C467A", "622F61", "4B1A4B", "380639", "210023"},
		[12]string{"210023", "380639", "4B1A4B", "622F61", "7C467A", "8F578C", "B175AC", "D394CD", "EEACE7", "FFC1F7", "FFDBF8", "FFF3F9"}},
	{"0B0B0B", 3.0229133609532077, 11, 0,
		[12]string{"F9F6F5", "E8E5E4", "D7D4D3", "C3C0C0", "AAA8A7", "8A8888", "6C6A6A", "5A5959", "434242", "2F2E2E", "1E1D1D", "0E0E0E"},
		[12]string{"0E0E0E", "1E1D1D", "2F2E2E", "434242", "5A5959", "6C6A6A", "8A8888", "AAA8A7", "C3C0C0", "D7D4D3", "E8E5E4", "F9F6F5"}},
	{"00FF00", 87.73703347354422, 2, 9,
		[12]string{"E2FFD4", "8DFF76", "01F600", "02E000", "03C400", "039F00", "027C00", "026900", "014E00", "013700", "002400", "001200"},
		[12]string{"001200", "002400", "013700", "014E00", "026900", "027C00", "039F00", "03C400", "02E000", "01F600", "8DFF76", "E2FFD4"}},
	{"0000FF", 32.302586667249486, 8, 3,
		[12]string{"F8F5FF", "E4E3FF", "CFD1FF", "B8BCFF", "9AA0FF", "727BFF", "4752FF", "2A33FF", "0000E1", "0001A5", "000074", "000046"},
		[12]string{"000046", "000074", "0001A5", "0000E1", "2A33FF", "4752FF", "727BFF", "9AA0FF", "B8BCFF", "CFD1FF", "E4E3FF", "F8F5FF"}},
	{"FF0000", 53.23288178584245, 5, 6,
		[12]string{"FFF4F2", "FFDED9", "FFC7BE", "FFAC9F", "FF8573", "FF3E2C", "D80100", "B70100", "8A0100", "640000", "450000", "270000"},
		[12]string{"270000", "450000", "640000", "8A0100", "B70100", "D80100", "FF3E2C", "FF8573", "FFAC9F", "FFC7BE", "FFDED9", "FFF4F2"}},
	{"1264A3", 41.03641394365418, 7, 4,
		[12]string{"F3F6FF", "D6E7FF", "B8D7FF", "92C5FF", "6DADF0", "4B8DCE", "246EAD", "005C9A", "004474", "003054", "001F39", "000F20"},
		[12]string{"000F20", "001F39", "003054", "004474", "005C9A", "246EAD", "4B8DCE", "6DADF0", "92C5FF", "B8D7FF", "D6E7FF", "F3F6FF"}},
	{"ECB22E", 75.98995992451252, 3, 8,
		[12]string{"FFF5E9", "FFE2AE", "FFCD6A", "F2B734", "D79F18", "B08100", "8A6400", "755400", "573E00", "3E2B00", "291B00", "150D00"},
		[12]string{"150D00", "291B00", "3E2B00", "573E00", "755400", "8A6400", "B08100", "D79F18", "F2B734", "FFCD6A", "FFE2AE", "FFF5E9"}},
	{"7F7F80", 53.221246018209186, 5, 6,
		[12]string{"F7F6F7", "E6E5E6", "D5D4D5", "C1C1C2", "A9A8A9", "89898A", "6A6A6B", "59595A", "414243", "2D2E2F", "1D1E1F", "0D0E0F"},
		[12]string{"0D0E0F", "1D1E1F", "2D2E2F", "414243", "59595A", "6A6A6B", "89898A", "A9A8A9", "C1C1C2", "D5D4D5", "E6E5E6", "F7F6F7"}},
	{"20C3F5", 73.500000064376, 3, 8,
		[12]string{"EDF8FF", "C4EBFF", "95DEFF", "49CFFF", "00B6E7", "0094BC", "007494", "00617D", "00485D", "003343", "00212C", "001018"},
		[12]string{"001018", "00212C", "003343", "00485D", "00617D", "007494", "0094BC", "00B6E7", "49CFFF", "95DEFF", "C4EBFF", "EDF8FF"}},
	{"98D8AA", 81.12057498370818, 3, 8,
		[12]string{"DDFFE3", "B3F4C5", "A3E3B4", "90CFA2", "78B68B", "58966D", "3A7650", "276540", "084D2A", "00361B", "002310", "001206"},
		[12]string{"001206", "002310", "00361B", "084D2A", "276540", "3A7650", "58966D", "78B68B", "90CFA2", "A3E3B4", "B3F4C5", "DDFFE3"}},
	{"4C9C9E", 59.69132616749515, 5, 6,
		[12]string{"D4FEFF", "A4F3F5", "93E2E4", "7FCED0", "66B5B7", "449597", "1D7678", "006466", "004A4C", "003436", "002223", "001112"},
		[12]string{"001112", "002223", "003436", "004A4C", "006466", "1D7678", "449597", "66B5B7", "7FCED0", "93E2E4", "A4F3F5", "D4FEFF"}},
	{"8AD3A6", 78.89414543189636, 3, 8,
		[12]string{"DCFFE5", "ABF6C6", "9BE4B6", "88D0A4", "6FB78C", "50976E", "2F7852", "1A6642", "004D2D", "00361F", "002312", "001207"},
		[12]string{"001207", "002312", "00361F", "004D2D", "1A6642", "2F7852", "50976E", "6FB78C", "88D0A4", "9BE4B6", "ABF6C6", "DCFFE5"}},
	{"FCB20E", 77.6192122164696, 3, 8,
		[12]string{"FFF5EB", "FFE1B6", "FFCC7A", "FDB310", "DE9C00", "B57F00", "8E6200", "785200", "5A3D00", "402A00", "2A1B00", "160C00"},
		[12]string{"160C00", "2A1B00", "402A00", "5A3D00", "785200", "8E6200", "B57F00", "DE9C00", "FDB310", "FFCC7A", "FFE1B6", "FFF5EB"}},
	{"AC1458", 37.79851010289377, 7, 4,
		[12]string{"FFF4F5", "FFDDE4", "FFC5D3", "FFA8C0", "FF7FA8", "ED4C88", "C42B6A", "AD1558", "860041", "61002E", "42001D", "26000E"},
		[12]string{"26000E", "42001D", "61002E", "860041", "AD1558", "C42B6A", "ED4C88", "FF7FA8", "FFA8C0", "FFC5D3", "FFDDE4", "FFF4F5"}},
	{"8779D8", 55.870384096817205, 5, 6,
		[12]string{"FAF4FF", "E8E1FF", "D7CEFF", "C3B8FF", "AA9CFE", "8A7CDB", "6B5DBA", "5A4CA7", "42338E", "2E1B79", "1C0069", "0D003F"},
		[12]string{"0D003F", "1C0069", "2E1B79", "42338E", "5A4CA7", "6B5DBA", "8A7CDB", "AA9CFE", "C3B8FF", "D7CEFF", "E8E1FF", "FAF4FF"}},
	{"DEB158", 74.6216683717995, 3, 8,
		[12]string{"FFF5E9", "FFE2AF", "FDCD71", "E8BA60", "CDA24B", "AB832E", "89640F", "755400", "573E00", "3E2B00", "291B00", "160D00"},
		[12]string{"160D00", "291B00", "3E2B00", "573E00", "755400", "89640F", "AB832E", "CDA24B", "E8BA60", "FDCD71", "FFE2AF", "FFF5E9"}},
	{"EA52D8", 60.32429228370965, 5, 6,
		[12]string{"FFF3F8", "FFDBF3", "FFC2EF", "FFA2EC", "FF72EB", "DF48CE", "BA21AD", "A30099", "7B0073", "590053", "3C0038", "22001F"},
		[12]string{"22001F", "3C0038", "590053", "7B0073", "A30099", "BA21AD", "DF48CE", "FF72EB", "FFA2EC", "FFC2EF", "FFDBF3", "FFF3F8"}},
	{"131658", 12.069343306389506, 10, 1,
		[12]string{"F8F5FF", "E4E3FF", "D0D1FF", "B8BCFF", "9FA3EB", "7F83C9", "6064A8", "4F5395", "383B7C", "232667", "101356", "010046"},
		[12]string{"010046", "101356", "232667", "383B7C", "4F5395", "6064A8", "7F83C9", "9FA3EB", "B8BCFF", "D0D1FF", "E4E3FF", "F8F5FF"}},
	{"FF53D8", 63.67570210310447, 4, 7,
		[12]string{"FFF4F7", "FFDCEF", "FFC3E8", "FFA4E1", "FF76DA", "E93FC5", "C210A4", "A7008C", "7E0069", "5B004C", "3E0033", "23001C"},
		[12]string{"23001C", "3E0033", "5B004C", "7E0069", "A7008C", "C210A4", "E93FC5", "FF76DA", "FFA4E1", "FFC3E8", "FFDCEF", "FFF4F7"}},
	{"35AA90", 62.94412862142612, 5, 6,
		[12]string{"D8FFF1", "8BF9DC", "7AE8CB", "65D4B8", "49BBA0", "1C9A81", "007964", "006654", "004C3E", "00352B", "00231B", "00110D"},
		[12]string{"00110D", "00231B", "00352B", "004C3E", "006654", "007964", "1C9A81", "49BBA0", "65D4B8", "7AE8CB", "8BF9DC", "D8FFF1"}},
	{"317FB0", 50.64775641467183, 6, 5,
		[12]string{"F1F7FF", "D0E8FF", "ADDAFF", "82C8FD", "68AFE3", "458FC1", "1C71A1", "005F8C", "004669", "00314B", "002033", "00101C"},
		[12]string{"00101C", "002033", "00314B", "004669", "005F8C", "1C71A1", "458FC1", "68AFE3", "82C8FD", "ADDAFF", "D0E8FF", "F1F7FF"}},
}

func parseHex(t *testing.T, s string) color.NRGBA {
	t.Helper()
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil || len(s) != 6 {
		t.Fatalf("bad hex %q", s)
	}
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
}

func hexString(c color.NRGBA) string {
	if c.A != 0xff {
		return fmt.Sprintf("%02X%02X%02X/a%02X", c.R, c.G, c.B, c.A)
	}
	return fmt.Sprintf("%02X%02X%02X", c.R, c.G, c.B)
}

func TestHCTCustomRamp(t *testing.T) {
	for _, v := range hctVectors {
		in := parseHex(t, v.hex)
		for _, mode := range []struct {
			dark bool
			want [12]string
		}{{false, v.light}, {true, v.dark}} {
			got := CustomRamp(in, mode.dark)
			for i := range got {
				if g := hexString(got[i]); g != mode.want[i] {
					t.Errorf("CustomRamp(#%s, dark=%v)[%d] = %s, want %s", v.hex, mode.dark, i, g, mode.want[i])
				}
			}
		}
	}
}

func TestHCTCustomKeyShade(t *testing.T) {
	for _, v := range hctVectors {
		in := parseHex(t, v.hex)
		if g := CustomKeyShade(in, false); g != v.keyLight {
			t.Errorf("CustomKeyShade(#%s, light) = %d, want %d", v.hex, g, v.keyLight)
		}
		if g := CustomKeyShade(in, true); g != v.keyDark {
			t.Errorf("CustomKeyShade(#%s, dark) = %d, want %d", v.hex, g, v.keyDark)
		}
	}
}

func TestHCTHexTone(t *testing.T) {
	for _, v := range hctVectors {
		// Go's math.Pow is not V8's fdlibm pow, so the tone can differ in the
		// last ulp. No sRGB value's tone lies within 6e-8 of 66 or of a midpoint
		// between ramp tones, so that never changes a decision.
		if g := HexTone(parseHex(t, v.hex)); math.Abs(g-v.tone) > 1e-12 {
			t.Errorf("HexTone(#%s) = %v, want %v", v.hex, g, v.tone)
		}
	}
}

func TestHCTIgnoresAlpha(t *testing.T) {
	c := color.NRGBA{R: 0x4C, G: 0x64, B: 0xA6, A: 0x10}
	o := c
	o.A = 0xff
	if CustomRamp(c, false) != CustomRamp(o, false) || HexTone(c) != HexTone(o) {
		t.Error("alpha changed the ramp or tone")
	}
}
