package diagnostic

const TokenEndpoint = "/apizt/opentoken/oauth/token"

// Authentication describes token acquisition separately from the data-page
// attempt. Response contains only a bounded, redacted error summary, never
// authentication parameters, a successful token response or transport errors.
type Authentication struct {
	Endpoint   string   `json:"endpoint"`
	Reason     string   `json:"reason"`
	HTTPStatus int      `json:"http_status,omitempty"`
	Response   Response `json:"response,omitzero"`
}

func (a Authentication) Valid() bool {
	if a.Endpoint != TokenEndpoint || !a.Response.Valid() || (a.HTTPStatus != 0 && (a.HTTPStatus < 100 || a.HTTPStatus > 599)) {
		return false
	}
	switch a.Reason {
	case "configuration", "transport", "timeout", "cancelled", "http", "read", "response_limit", "structure", "business", "invalid_token", "expired_token":
		return true
	default:
		return false
	}
}
