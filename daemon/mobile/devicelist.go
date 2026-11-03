package mobile

// markCurrentDevices drops the identity keys and flags this install's row:
// current only for a non-empty myPub equal to the row's key.
func markCurrentDevices(rows []hubDevice, myPub string) []deviceInfo {
	out := make([]deviceInfo, 0, len(rows))
	for _, row := range rows {
		out = append(out, deviceInfo{
			ID:           row.ID,
			FriendlyName: row.FriendlyName,
			CreatedAt:    row.CreatedAt,
			Status:       row.Status,
			Current:      myPub != "" && row.IdentityPubkey != nil && *row.IdentityPubkey == myPub,
		})
	}
	return out
}
