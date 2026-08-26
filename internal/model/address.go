package model

type Address struct {
	ID            string
	UserID        string
	Label         *string
	RecipientName string
	Phone         string
	FullAddress   string
	City          string
	Province      string
	PostalCode    string
	IsDefault     bool
}
