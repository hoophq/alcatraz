// Package entities defines the canonical entity-type identifiers alcatraz can
// detect. The names follow the widely used SCREAMING_SNAKE_CASE convention for
// PII entity types so reports and downstream severity maps stay compatible
// across implementations.
package entities

// Generic entities (language-independent).
const (
	Person       = "PERSON"
	EmailAddress = "EMAIL_ADDRESS"
	PhoneNumber  = "PHONE_NUMBER"
	CreditCard   = "CREDIT_CARD"
	Crypto       = "CRYPTO"
	DateTime     = "DATE_TIME"
	IBANCode     = "IBAN_CODE"
	IPAddress    = "IP_ADDRESS"
	Location     = "LOCATION"
	NRP          = "NRP"
	URL          = "URL"
)

// United States.
const (
	USSSN           = "US_SSN"
	USDriverLicense = "US_DRIVER_LICENSE"
	USPassport      = "US_PASSPORT"
	USBankNumber    = "US_BANK_NUMBER"
	USITIN          = "US_ITIN"
	ABARouting      = "ABA_ROUTING"
	MedicalLicense  = "MEDICAL_LICENSE"
)

// United Kingdom.
const (
	UKNHS  = "UK_NHS"
	UKNINO = "UK_NINO"
)

// Australia.
const (
	AUTFN      = "AU_TFN"
	AUABN      = "AU_ABN"
	AUACN      = "AU_ACN"
	AUMedicare = "AU_MEDICARE"
)

// India.
const (
	INAadhaar             = "IN_AADHAAR"
	INPAN                 = "IN_PAN"
	INPassport            = "IN_PASSPORT"
	INVehicleRegistration = "IN_VEHICLE_REGISTRATION"
	INVoter               = "IN_VOTER"
	INGSTIN               = "IN_GSTIN"
)

// Italy.
const (
	ITFiscalCode    = "IT_FISCAL_CODE"
	ITVATCode       = "IT_VAT_CODE"
	ITIdentityCard  = "IT_IDENTITY_CARD"
	ITDriverLicense = "IT_DRIVER_LICENSE"
	ITPassport      = "IT_PASSPORT"
)

// Spain.
const (
	ESNIF = "ES_NIF"
	ESNIE = "ES_NIE"
)

// Singapore.
const (
	SGFIN = "SG_FIN"
	SGUEN = "SG_UEN"
)

// Brazil.
const (
	BRCPF  = "BR_CPF"
	BRCNPJ = "BR_CNPJ"
	BRRG   = "BR_RG"
	BRCNH  = "BR_CNH"
	BRPIS  = "BR_PIS" // also PASEP / NIS / NIT
)

// Other national identifiers.
const (
	PLPESEL                = "PL_PESEL"
	KRRRN                  = "KR_RRN"
	FIPersonalIdentityCode = "FI_PERSONAL_IDENTITY_CODE"
	THTNIN                 = "TH_TNIN"
)
