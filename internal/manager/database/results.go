package database

import (
	"fmt"
	"strings"
)

// CutGroupPrefix checks if a result string represents a grouped result.
// Returns the grouped value (without the "grouped:" prefix) and true if it's a grouped result.
func CutGroupPrefix(result string) (string, bool) {
	return strings.CutPrefix(result, "grouped:")
}

// GetFirstGroupedResultID extracts the first result ID from a grouped result value.
func GetFirstGroupedResultID(groupedValue string) string {
	res := strings.Split(groupedValue, ",")
	return res[0]
}

// ExtractRequestIDFromResult determines the appropriate request ID for a given result.
// For grouped results, it uses the result ID directly.
// For individual results, it follows the groupID vs individual ID logic.
func ExtractRequestIDFromResult(result string, resultID string) (string, error) {
	if _, isGrouped := CutGroupPrefix(result); isGrouped {
		return fmt.Sprintf("%s:%s", RequestKeyPrefix, resultID), nil
	}

	task, err := UnmarshalTask([]byte(result))
	if err != nil {
		return "", err
	}

	switch {
	case task.Result == nil:
		return fmt.Sprintf("%s:%s", RequestKeyPrefix, resultID), nil
	case task.Result.GetGroupID() != 0:
		return fmt.Sprintf("%s:%d", RequestKeyPrefix, task.Result.GetGroupID()), nil
	case task.Result.GetId() != 0:
		return fmt.Sprintf("%s:%d", RequestKeyPrefix, task.Result.GetId()), nil
	default:
		return fmt.Sprintf("%s:%s", RequestKeyPrefix, resultID), nil
	}
}
