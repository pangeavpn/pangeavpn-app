//go:build !darwin

package egress

func RemoveOrphanedRoutes(func(format string, args ...any)) {}
