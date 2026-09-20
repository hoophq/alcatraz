# What it detects

52 entity types. ✓ marks a checksum or format validator, so those recognizers
confirm an identifier rather than matching its shape. Constants live in the
[`entities`](https://pkg.go.dev/github.com/hoophq/alcatraz/entities) package.

| Group | Entity types |
|-------|--------------|
| Generic | `EMAIL_ADDRESS`, `PHONE_NUMBER`, `CREDIT_CARD`✓, `CRYPTO`, `IP_ADDRESS`, `URL`, `DATE_TIME`, `IBAN_CODE`✓ |
| United States | `US_SSN`✓, `US_ITIN`✓, `US_PASSPORT`, `US_DRIVER_LICENSE`, `US_BANK_NUMBER`, `ABA_ROUTING`✓, `MEDICAL_LICENSE` |
| United Kingdom | `UK_NHS`✓, `UK_NINO`✓ |
| China | `CN_ID_CARD`✓ |
| Australia | `AU_TFN`✓, `AU_ABN`✓, `AU_ACN`✓, `AU_MEDICARE`✓ |
| India | `IN_AADHAAR`✓, `IN_PAN`, `IN_PASSPORT`, `IN_VEHICLE_REGISTRATION`, `IN_VOTER`, `IN_GSTIN` |
| Italy | `IT_FISCAL_CODE`✓, `IT_VAT_CODE`✓, `IT_IDENTITY_CARD`, `IT_DRIVER_LICENSE`, `IT_PASSPORT` |
| Spain | `ES_NIF`✓, `ES_NIE`✓ |
| Singapore | `SG_FIN`✓, `SG_UEN` |
| Brazil | `BR_CPF`✓, `BR_CNPJ`✓, `BR_RG`, `BR_CNH`✓, `BR_PIS`✓, `BR_CNS`✓, `BR_TITULO_ELEITORAL`✓, `BR_RENAVAM`✓, `BR_CEP`, `BR_PLACA`, `BR_PIX_KEY` |
| Other | `PL_PESEL`✓, `KR_RRN`✓, `FI_PERSONAL_IDENTITY_CODE`✓, `TH_TNIN`✓ |

Every built-in detects a **language-independent** structured identifier: an
IBAN or a Thai national ID looks the same in any surrounding text. The complete
set therefore stays active under whichever language you build an engine with.
The language key exists for language-specific recognizers, such as the
[`ner`](ner.md) module's model-backed recognizer.

`CN_ID_CARD` covers the 18-character citizen identification number in
[GB 11643-1999](https://openstd.samr.gov.cn/bzgk/std/newGbInfo?hcno=080D6FBF2BB468F9007657F26D60013E):
six address digits, an eight-digit calendar date, three sequence digits and a
MOD 11-2 check character. It accepts a transcribed lowercase `x`, preserves
byte offsets next to Chinese text, and never joins columns or lines. It does
not cover legacy 15-digit IDs, verify issuance or ownership, or validate
historical address-code assignments. Checksum-valid findings are not proof
that an identifier belongs to a real person. Tests use Appendix A examples
and generated fixtures, not customer data.

## Not in this list

`PERSON`, `LOCATION`, `NRP` and free-text `DATE_TIME` need a statistical model
rather than a regex. They come from the optional [`ner`](ner.md) or
[`pfilter`](pfilter.md) modules; the core alone never emits them.
