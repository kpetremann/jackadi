package database

import (
	"fmt"
	"strings"
)

// StringToKey parses a database key string into its prefix and ID components.
func StringToKey(key string) (Key, error) {
	keyParts := strings.Split(key, ":")
	if len(keyParts) != 2 {
		return Key{}, fmt.Errorf("invalid key: %s not in 'prefix.id' format", key)
	}
	return Key{keyParts[0], keyParts[1]}, nil
}

// GenerateResultKey creates a database key for storing task results.
func GenerateResultKey(id string) []byte {
	return fmt.Appendf(nil, "%s:%s", ResultKeyPrefix, id)
}

// GenerateRequestKey creates a database key for storing task requests using an integer ID.
func GenerateRequestKey(id int64) []byte {
	return fmt.Appendf(nil, "%s:%d", RequestKeyPrefix, id)
}

// GenerateRequestKeyFromString creates a database key for storing task requests using a string ID.
func GenerateRequestKeyFromString(id string) []byte {
	return fmt.Appendf(nil, "%s:%s", RequestKeyPrefix, id)
}
