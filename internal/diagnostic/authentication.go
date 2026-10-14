package diagnostic

const TokenEndpoint = "/apizt/opentoken/oauth/token"

// Authentication describes token acquisition separately from the data-page
// attempt. It never contains a query string, response body or transport error.
type Authentication struct {
	Endpoint   string `json:"endpoint"`
	Reason     string `json:"reason"`
	HTTPStatus int    `json:"http_status,omitempty"`
}

func (a Authentication) Valid() bool {
	if a.Endpoint != TokenEndpoint || (a.HTTPStatus != 0 && (a.HTTPStatus < 100 || a.HTTPStatus > 599)) {
		return false
	}
	switch a.Reason {
	case "configuration", "transport", "timeout", "cancelled", "http", "read", "response_limit", "structure", "business", "invalid_token", "expired_token":
		return true
	default:
		return false
	}
}
