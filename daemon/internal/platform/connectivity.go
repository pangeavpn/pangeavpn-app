package platform

import "errors"

// ErrNoDefaultRoute: the route table was readable and no physical interface
// holds a default route.
var ErrNoDefaultRoute = errors.New("no physical default route")

func hostInternetFromRoute(err error) (online bool, known bool) {
	switch {
	case err == nil:
		return true, true
	case errors.Is(err, ErrNoDefaultRoute):
		return false, true
	}
	return false, false
}
