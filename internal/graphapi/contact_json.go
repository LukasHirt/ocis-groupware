package graphapi

// contactJSON is a Graph contact. Only the fields the facade maps are present;
// text fields that the card does not have are empty strings, lists are empty
// and birthday is null.
type contactJSON struct {
	ID                   string          `json:"id"`
	ETag                 string          `json:"@odata.etag,omitempty"`
	ChangeKey            string          `json:"changeKey,omitempty"`
	ParentFolderID       string          `json:"parentFolderId,omitempty"`
	LastModifiedDateTime string          `json:"lastModifiedDateTime,omitempty"`
	Categories           []string        `json:"categories"`
	Birthday             *string         `json:"birthday"`
	DisplayName          string          `json:"displayName"`
	GivenName            string          `json:"givenName"`
	MiddleName           string          `json:"middleName"`
	Surname              string          `json:"surname"`
	NickName             string          `json:"nickName"`
	Title                string          `json:"title"`
	Generation           string          `json:"generation"`
	YomiGivenName        string          `json:"yomiGivenName"`
	YomiSurname          string          `json:"yomiSurname"`
	YomiCompany          string          `json:"yomiCompany"`
	JobTitle             string          `json:"jobTitle"`
	CompanyName          string          `json:"companyName"`
	Department           string          `json:"department"`
	Profession           string          `json:"profession"`
	AssistantName        string          `json:"assistantName"`
	Manager              string          `json:"manager"`
	SpouseName           string          `json:"spouseName"`
	Children             []string        `json:"children"`
	BusinessPhones       []string        `json:"businessPhones"`
	HomePhones           []string        `json:"homePhones"`
	MobilePhone          string          `json:"mobilePhone"`
	BusinessHomePage     string          `json:"businessHomePage"`
	PersonalNotes        string          `json:"personalNotes"`
	EmailAddresses       []emailAddress  `json:"emailAddresses"`
	IMAddresses          []string        `json:"imAddresses"`
	HomeAddress          physicalAddress `json:"homeAddress"`
	BusinessAddress      physicalAddress `json:"businessAddress"`
	OtherAddress         physicalAddress `json:"otherAddress"`
	Groups               []contactGroup  `json:"groupware.groups"`
}

// physicalAddress is a Graph physicalAddress.
type physicalAddress struct {
	Street          string `json:"street"`
	City            string `json:"city"`
	State           string `json:"state"`
	CountryOrRegion string `json:"countryOrRegion"`
	PostalCode      string `json:"postalCode"`
}

func (a physicalAddress) empty() bool { return a == physicalAddress{} }

// contactGroup is a group card that lists the contact as a member. It is
// shown and never written.
type contactGroup struct {
	// ID is the UID of the group card.
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

// contactPatch is the body of a POST or PATCH of a contact. An absent field
// is left alone, and null or an empty value clears it.
type contactPatch struct {
	Categories       opt[[]string]        `json:"categories"`
	Birthday         opt[string]          `json:"birthday"`
	DisplayName      opt[string]          `json:"displayName"`
	GivenName        opt[string]          `json:"givenName"`
	MiddleName       opt[string]          `json:"middleName"`
	Surname          opt[string]          `json:"surname"`
	NickName         opt[string]          `json:"nickName"`
	Title            opt[string]          `json:"title"`
	Generation       opt[string]          `json:"generation"`
	YomiGivenName    opt[string]          `json:"yomiGivenName"`
	YomiSurname      opt[string]          `json:"yomiSurname"`
	YomiCompany      opt[string]          `json:"yomiCompany"`
	JobTitle         opt[string]          `json:"jobTitle"`
	CompanyName      opt[string]          `json:"companyName"`
	Department       opt[string]          `json:"department"`
	Profession       opt[string]          `json:"profession"`
	AssistantName    opt[string]          `json:"assistantName"`
	Manager          opt[string]          `json:"manager"`
	SpouseName       opt[string]          `json:"spouseName"`
	Children         opt[[]string]        `json:"children"`
	BusinessPhones   opt[[]string]        `json:"businessPhones"`
	HomePhones       opt[[]string]        `json:"homePhones"`
	MobilePhone      opt[string]          `json:"mobilePhone"`
	BusinessHomePage opt[string]          `json:"businessHomePage"`
	PersonalNotes    opt[string]          `json:"personalNotes"`
	EmailAddresses   opt[[]emailAddress]  `json:"emailAddresses"`
	IMAddresses      opt[[]string]        `json:"imAddresses"`
	HomeAddress      opt[physicalAddress] `json:"homeAddress"`
	BusinessAddress  opt[physicalAddress] `json:"businessAddress"`
	OtherAddress     opt[physicalAddress] `json:"otherAddress"`
}
