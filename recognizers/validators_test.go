package recognizers

import "testing"

// checkValidator asserts that fn accepts every value in valid and rejects every
// value in invalid.
func checkValidator(t *testing.T, name string, fn func(string) bool, valid, invalid []string) {
	t.Helper()
	for _, v := range valid {
		if !fn(v) {
			t.Errorf("%s: expected %q to be VALID", name, v)
		}
	}
	for _, v := range invalid {
		if fn(v) {
			t.Errorf("%s: expected %q to be INVALID", name, v)
		}
	}
}

func TestValidators(t *testing.T) {
	cases := []struct {
		name    string
		fn      func(string) bool
		valid   []string
		invalid []string
	}{
		{
			"luhn", luhnValid,
			[]string{"4532015112830366", "4111111111111111", "79927398713"},
			[]string{"4532015112830367", "1234567812345678"},
		},
		{
			"iban", validateIBAN,
			[]string{"DE89370400440532013000", "GB82WEST12345698765432", "DE89 3704 0044 0532 0130 00"},
			[]string{"DE89370400440532013001", "XX00", "1234567890123456"},
		},
		{
			"ssn", validateSSN,
			[]string{"536-90-4399", "078-05-1120", "457554462"},
			[]string{"123-45-6789", "000-12-3456", "666-12-3456", "900-12-3456", "536-00-4399", "111111111"},
		},
		{
			"itin", validateITIN,
			[]string{"970-12-3456", "950-70-1234"},
			[]string{"123-45-6789", "966-12-3456", "970-00-3456", "970-12-0000"},
		},
		{
			"aba", validateABA,
			[]string{"021000021", "011401533"},
			[]string{"021000022", "12345678"},
		},
		{
			"nhs", validateNHS,
			[]string{"9434765919", "943 476 5919"},
			[]string{"9434765918", "123456789"},
		},
		{
			"nino", validateNINO,
			[]string{"AB123456C", "AB 12 34 56 C"},
			[]string{"BG123456C", "AB123456E", "AB12345C"},
		},
		{
			"au_tfn", validateTFN,
			[]string{"123456782", "123 456 782"},
			[]string{"123456789", "12345678"},
		},
		{
			"au_abn", validateABN,
			[]string{"51824753556", "51 824 753 556"},
			[]string{"51824753557", "1234567890"},
		},
		{
			"au_acn", validateACN,
			[]string{"004085616"},
			[]string{"004085617", "12345678"},
		},
		{
			"au_medicare", validateMedicare,
			[]string{"2123456701"},
			[]string{"7123456701", "2123456711", "123456789"},
		},
		{
			"verhoeff", verhoeffValid,
			[]string{"234123412346", "2363"},
			[]string{"234123412347", "2364"},
		},
		{
			"it_fiscal_code", validateITFiscalCode,
			[]string{"RSSMRA85T10A562C"},
			[]string{"RSSMRA85T10A562X", "RSSMRA85T10A562", "TOOSHORT"},
		},
		{
			"it_vat", validateITVAT,
			[]string{"00743110157"},
			[]string{"00743110158", "1234567890"},
		},
		{
			"es_nif", validateESNIF,
			[]string{"12345678Z"},
			[]string{"12345678A", "1234567Z"},
		},
		{
			"es_nie", validateESNIE,
			[]string{"X1234567L"},
			[]string{"X1234567A", "A1234567L"},
		},
		{
			"sg_fin", validateSGFIN,
			[]string{"F1234567N"},
			[]string{"F1234567A", "A1234567N"},
		},
		{
			"pl_pesel", validatePESEL,
			[]string{"44051401359"},
			[]string{"44051401358", "1234567890"},
		},
		{
			"kr_rrn", validateKRRRN,
			[]string{"900101-1234568"},
			[]string{"900101-1234567", "12345678"},
		},
		{
			"fi_personal_code", validateFIPersonalCode,
			[]string{"131052-308T"},
			[]string{"131052-308A", "131052X308T", "131052-308"},
		},
		{
			"th_tnin", validateTHTNIN,
			[]string{"1101700230252", "1-1017-00230-25-2"},
			[]string{"1101700230251", "12345"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			checkValidator(t, c.name, c.fn, c.valid, c.invalid)
		})
	}
}
