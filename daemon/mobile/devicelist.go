package mobile

import "encoding/json"

// hubDeviceList.NameKey is set, to null at least, only by a hub that speaks names v2.
type hubDeviceList struct {
	Devices []hubDevice     `json:"devices"`
	NameKey json.RawMessage `json:"nameKey"`
}

type sealedRename struct {
	DeviceID   string `json:"deviceId"`
	SealedName string `json:"sealedName"`
	NameKey    string `json:"nameKey,omitempty"`
}

func (l hubDeviceList) speaksNamesV2() bool { return len(l.NameKey) > 0 }

func (l hubDeviceList) wrappedNameKey() string {
	var wrapped string
	_ = json.Unmarshal(l.NameKey, &wrapped)
	return wrapped
}

// shownRows puts each row's display name in FriendlyName; an old hub's rows pass through.
func (l hubDeviceList) shownRows(credential string) []hubDevice {
	if !l.speaksNamesV2() {
		return l.Devices
	}
	k := unwrapNameKey(numberWrapKey(credential), l.wrappedNameKey())
	rows := make([]hubDevice, len(l.Devices))
	for i, row := range l.Devices {
		row.FriendlyName = displayName(row, k)
		rows[i] = row
	}
	return rows
}

// displayName is open(sealedName) ?? label ?? friendlyName ?? short id, a blank counting as missing.
func displayName(row hubDevice, k []byte) *string {
	if row.SealedName != nil {
		if name, ok := openName(k, row.ID, *row.SealedName); ok {
			return &name
		}
	}
	for _, name := range []*string{row.Label, row.FriendlyName} {
		if name != nil && *name != "" {
			return name
		}
	}
	short := row.ID
	if len(short) > 8 {
		short = short[:8]
	}
	return &short
}

// sealRename seals name under the account's key, creating that key when the hub has none.
func (l hubDeviceList) sealRename(credential, deviceID, name string) (sealedRename, error) {
	wrapKey := numberWrapKey(credential)
	if wrapKey == nil {
		return sealedRename{}, errRenameNeedsAccountNumber
	}
	body := sealedRename{DeviceID: deviceID}
	var k []byte
	var err error
	if wrapped := l.wrappedNameKey(); wrapped != "" {
		if k = unwrapNameKey(wrapKey, wrapped); k == nil {
			return sealedRename{}, errNameKeyUnreadable
		}
	} else {
		k = newNameKey()
		if body.NameKey, err = wrapNameKey(wrapKey, k); err != nil {
			return sealedRename{}, err
		}
	}
	body.SealedName, err = sealName(k, deviceID, name)
	return body, err
}

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
