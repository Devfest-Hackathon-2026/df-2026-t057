package account

// ExportCollections contains user-owned records; credentials and operation receipts are never exported.
var ExportCollections = []string{"state", "tasks", "archivedTasks", "ledger", "viewingSessions", "completionRecords", "activityEvents", "assistantTurns"}

func Exportable(collection, id string) bool {
	if collection != "state" {
		return true
	}
	switch id {
	case "profile", "settings", "sleep", "session", "wallet", "viewing", "appOpens":
		return true
	default:
		return false
	}
}
func ExportRow(collection, id string, row map[string]any) map[string]any {
	row["_id"] = id
	if collection == "assistantTurns" {
		delete(row, "runId")
		delete(row, "pendingUntil")
	}
	return row
}
