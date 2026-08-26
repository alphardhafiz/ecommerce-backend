package service

import (
	"context"
	"regexp"

	"ecommerce/server/internal/model"
	"ecommerce/server/internal/repository"
)

var postalCodeRe = regexp.MustCompile(`^[0-9]{5}$`)

type AddressService struct {
	addresses *repository.AddressRepo
}

func NewAddressService(addresses *repository.AddressRepo) *AddressService {
	return &AddressService{addresses: addresses}
}

// validate checks PRD C.7: all fields required except label; postal_code
// must be numeric. Length caps match the schema's VARCHAR limits.
func validateAddress(in *model.Address) error {
	var verrs []FieldError
	add := func(field, msg string) { verrs = append(verrs, FieldError{Field: field, Message: msg}) }

	if in.RecipientName == "" || len(in.RecipientName) > 100 {
		add("recipient_name", "Recipient name is required (max 100 chars)")
	}
	if in.Phone == "" || len(in.Phone) > 20 {
		add("phone", "Phone is required (max 20 chars)")
	}
	if in.FullAddress == "" {
		add("full_address", "Full address is required")
	}
	if in.City == "" || len(in.City) > 100 {
		add("city", "City is required (max 100 chars)")
	}
	if in.Province == "" || len(in.Province) > 100 {
		add("province", "Province is required (max 100 chars)")
	}
	if !postalCodeRe.MatchString(in.PostalCode) {
		add("postal_code", "Postal code must be 5 digits")
	}
	if in.Label != nil && len(*in.Label) > 50 {
		add("label", "Label max 50 chars")
	}
	if len(verrs) > 0 {
		return &ValidationError{Errors: verrs}
	}
	return nil
}

func (s *AddressService) List(ctx context.Context, userID string) ([]*model.Address, error) {
	return s.addresses.List(ctx, userID)
}

func (s *AddressService) Create(ctx context.Context, userID string, in *model.Address) (*model.Address, error) {
	if err := validateAddress(in); err != nil {
		return nil, err
	}
	return s.addresses.Create(ctx, userID, in)
}

// Update returns ErrForbidden when the address belongs to another user.
func (s *AddressService) Update(ctx context.Context, userID, id string, in *model.Address) error {
	if err := validateAddress(in); err != nil {
		return err
	}
	if err := s.ensureOwner(ctx, userID, id); err != nil {
		return err
	}
	return s.addresses.Update(ctx, userID, id, in)
}

// Delete returns ErrForbidden when the address belongs to another user.
func (s *AddressService) Delete(ctx context.Context, userID, id string) error {
	if err := s.ensureOwner(ctx, userID, id); err != nil {
		return err
	}
	return s.addresses.Delete(ctx, userID, id)
}

// SetDefault returns ErrForbidden when the address belongs to another user.
func (s *AddressService) SetDefault(ctx context.Context, userID, id string) error {
	if err := s.ensureOwner(ctx, userID, id); err != nil {
		return err
	}
	return s.addresses.SetDefault(ctx, userID, id)
}

func (s *AddressService) ensureOwner(ctx context.Context, userID, id string) error {
	a, err := s.addresses.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if a.UserID != userID {
		return ErrForbidden
	}
	return nil
}
