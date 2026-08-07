# What it detects

45 entity types. ✓ marks a checksum or format validator, so those recognizers
confirm an identifier rather than matching its shape. Constants live in the
[`entities`](https://pkg.go.dev/github.com/hoophq/alcatraz/entities) package.

| Group | Entity types |
|-------|--------------|
| Generic | `EMAIL_ADDRESS`, `PHONE_NUMBER`, `CREDIT_CARD`✓, `CRYPTO`, `IP_ADDRESS`, `URL`, `DATE_TIME`, `IBAN_CODE`✓ |
| United States | `US_SSN`✓, `US_ITIN`✓, `US_PASSPORT`, `US_DRIVER_LICENSE`, `US_BANK_NUMBER`, `ABA_ROUTING`✓, `MEDICAL_LICENSE` |
| United Kingdom | `UK_NHS`✓, `UK_NINO`✓ |
| Australia | `AU_TFN`✓, `AU_ABN`✓, `AU_ACN`✓, `AU_MEDICARE`✓ |
| India | `IN_AADHAAR`✓, `IN_PAN`, `IN_PASSPORT`, `IN_VEHICLE_REGISTRATION`, `IN_VOTER`, `IN_GSTIN` |
| Italy | `IT_FISCAL_CODE`✓, `IT_VAT_CODE`✓, `IT_IDENTITY_CARD`, `IT_DRIVER_LICENSE`, `IT_PASSPORT` |
| Spain | `ES_NIF`✓, `ES_NIE`✓ |
| Singapore | `SG_FIN`✓, `SG_UEN` |
| Brazil | `BR_CPF`✓, `BR_CNPJ`✓, `BR_RG`, `BR_CNH`✓, `BR_PIS`✓ |
| Other | `PL_PESEL`✓, `KR_RRN`✓, `FI_PERSONAL_IDENTITY_CODE`✓, `TH_TNIN`✓ |

Every built-in detects a **language-independent** structured identifier: an
IBAN or a Thai national ID looks the same in any surrounding text. The complete
set therefore stays active under whichever language you build an engine with.
The language key exists for language-specific recognizers, such as the
[`ner`](ner.md) module's model-backed recognizer.

## Not in this list

`PERSON`, `LOCATION`, `NRP` and free-text `DATE_TIME` need a statistical model
rather than a regex. They come from the optional [`ner`](ner.md) or
[`pfilter`](pfilter.md) modules; the core alone never emits them.
