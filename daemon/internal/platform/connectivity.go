package platform

import "errors"

// ErrNoDefaultRoute: the route table was readable and no physical interface
// holds a default route.
var ErrNoDefaultRoute = errors.New("no physical default route")
