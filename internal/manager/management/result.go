package management

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/dgraph-io/badger/v4"
	"github.com/kpetremann/jackadi/internal/config"
	"github.com/kpetremann/jackadi/internal/manager/database"
	"github.com/kpetremann/jackadi/internal/node"
	"github.com/kpetremann/jackadi/internal/proto"
	"github.com/kpetremann/jackadi/internal/serializer"
)

// GetRequest searches for a request ID in the local KV store, and returns the request.
func (a *apiServer) GetRequest(ctx context.Context, req *proto.RequestRequest) (*proto.RequestResponse, error) {
	result := []byte{}
	key := database.GenerateRequestKeyFromString(req.GetRequestID())
	err := a.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}

		result, err = item.ValueCopy(nil)
		if err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	return &proto.RequestResponse{
		Request: string(result),
	}, nil
}

// GetResults searches for a task ID in the local KV store, and returns the result.
func (a *apiServer) GetResults(ctx context.Context, req *proto.ResultsRequest) (*proto.ResultsResponse, error) {
	result := []byte{}
	key := database.GenerateResultKey(req.GetResultID())
	err := a.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}

		result, err = item.ValueCopy(nil)
		if err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	return &proto.ResultsResponse{
		Result: string(result),
	}, nil
}

// ListResults returns the list of results with support for pagination and filtering.
//
// Supports:
// - Pagination through offset and limit parameters.
// - Date range filtering through from_date and to_date parameters.
// - Node filtering through targets parameter.
func (a *apiServer) ListResults(ctx context.Context, req *proto.ListResultsRequest) (*proto.ListResultsResponse, error) {
	resultEntries := []*proto.ResultEntry{}

	limit := int32(config.ResultsLimit)
	if req.Limit > 0 {
		limit = min(req.Limit, config.ResultsPageLimit)
	}

	err := a.db.View(func(txn *badger.Txn) error {
		// Set up the iterator options
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = true
		opts.Reverse = true

		targetMap := make(map[string]bool)
		if len(req.Targets) > 0 {
			for _, target := range req.Targets {
				targetMap[target] = true
			}
		}

		it := txn.NewIterator(opts)
		defer it.Close()

		if req.ToDate != nil && *req.ToDate > 0 {
			seekKey := database.GenerateResultKey(strconv.FormatInt(*req.ToDate, 10))
			it.Seek(seekKey)

			// Badger's Seek will position at the next key if exact match not found
			// Since we're in reverse order, check if we need to move to the next entry
			if it.Valid() {
				currentKey := string(it.Item().Key())
				k, err := database.StringToKey(currentKey)
				if err != nil {
					return fmt.Errorf("invalid key in database: %w", err)
				}

				currentID, err := strconv.ParseInt(k.ID, 10, 64)
				if err == nil && currentID > *req.ToDate {
					it.Next() // The current position is after our to_date, move to next (older) entry
				}
			}
		} else {
			it.Rewind() // If no upper bound, start from the most recent
		}

		var count, skipped int32

		for ; it.Valid(); it.Next() {
			if count >= limit {
				break
			}

			item := it.Item()
			key := string(item.Key())
			if !strings.HasPrefix(key, database.ResultKeyPrefix) {
				continue
			}

			// Parse key to extract ID from "prefix:id" format
			dbKey, err := database.StringToKey(key)
			if err != nil {
				continue
			}

			id, err := strconv.ParseInt(dbKey.ID, 10, 64)
			if err != nil {
				continue
			}

			// Handle filters

			if req.FromDate != nil && *req.FromDate > 0 && id < *req.FromDate {
				// Since results are in reverse chronological order,
				// once we pass the lower bound we can stop completely
				break
			}

			val, err := item.ValueCopy(nil)
			if err != nil {
				continue
			}

			// filter by target
			if len(req.Targets) > 0 {
				var dbTask struct{ Node node.ID } // partial deserialisation
				if err := serializer.JSON.Unmarshal(val, &dbTask); err != nil || !targetMap[string(dbTask.Node)] {
					continue
				}
			}

			if skipped < req.Offset {
				skipped++
				continue
			}

			// Do full processing

			var resultEntry *proto.ResultEntry

			if len(val) >= 8 && string(val[:8]) == "grouped:" {
				resultEntry = &proto.ResultEntry{
					Id:     id,
					Node:   string(val),
					Status: "success",
				}
				resultEntries = append(resultEntries, resultEntry)
				count++
				continue
			}

			var dbTask database.Task

			// full deserialization
			if err := serializer.JSON.Unmarshal(val, &dbTask); err != nil {
				resultEntry = &proto.ResultEntry{
					Id:     id,
					Status: "unknown",
				}
				resultEntries = append(resultEntries, resultEntry)
				count++
				continue
			}

			status := "unknown"
			internalError := proto.InternalError_UNKNOWN_ERROR
			var errorMsg string

			if dbTask.Result != nil {
				internalError = dbTask.Result.GetInternalError()
				errorMsg = dbTask.Result.GetError()

				switch {
				case internalError == proto.InternalError_OK && errorMsg == "":
					status = "success"
				case errorMsg != "":
					status = "error"
				case internalError != proto.InternalError_OK:
					status = "internal error"
				}
			}

			resultEntry = &proto.ResultEntry{
				Id:            id,
				Node:          string(dbTask.Node),
				Status:        status,
				InternalError: internalError,
				Error:         errorMsg,
			}

			resultEntries = append(resultEntries, resultEntry)
			count++
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return &proto.ListResultsResponse{
		Results: resultEntries,
	}, nil
}
