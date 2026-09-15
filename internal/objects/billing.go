package objects

import (
	"fmt"
	"io"
	"time"
)

// BillingModelSource is the single global billing and public model policy.
type BillingModelSource string

const (
	BillingModelSourceOriginal   BillingModelSource = "original"
	BillingModelSourceRedirected BillingModelSource = "redirected"
)

func (s BillingModelSource) Valid() bool {
	return s == BillingModelSourceOriginal || s == BillingModelSourceRedirected
}

// RequestBilling freezes the policy at reception and channel price per attempt.
// This internal snapshot is never exposed as a GraphQL input or output.
type RequestBilling struct {
	Source         BillingModelSource `json:"source"`
	InjectCost     bool               `json:"injectCost,omitempty"`
	OriginalModel  string             `json:"originalModel"`
	Price          *ModelPrice        `json:"price,omitempty"`
	PriceReference string             `json:"priceReference,omitempty"`
	At             time.Time          `json:"at"`
}

func (s BillingModelSource) MarshalGQL(w io.Writer) { _, _ = fmt.Fprintf(w, "%q", s) }
func (s *BillingModelSource) UnmarshalGQL(v any) error {
	value, ok := v.(string)
	if !ok || !BillingModelSource(value).Valid() {
		return fmt.Errorf("invalid billing model source")
	}
	*s = BillingModelSource(value)
	return nil
}
