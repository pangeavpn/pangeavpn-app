package mobile

// markCurrentDevices drops the identity keys and flags this install's row:
// current only for a non-empty myPub equal to the row's key.
func markCurrentDevices(rows []hubDevice, myPub string, nameKey []byte) []deviceInfo {
	out := make([]deviceInfo, 0, len(rows))
	for _, row := range rows {
		out = append(out, deviceInfo{
			ID:           row.ID,
			FriendlyName: shownDeviceName(row, nameKey),
			CreatedAt:    row.CreatedAt,
			Status:       row.Status,
			Current:      myPub != "" && row.IdentityPubkey != nil && *row.IdentityPubkey == myPub,
		})
	}
	return out
}

func shownDeviceName(row hubDevice, nameKey []byte) *string {
	if row.EncryptedName != nil {
		if name, ok := openDeviceName(nameKey, *row.EncryptedName); ok {
			return &name
		}
	}
	return row.FriendlyName
}
