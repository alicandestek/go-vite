package chain_state

import (
	"bytes"
	"encoding/binary"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/util"
	"github.com/vitelabs/go-vite/chain/block"
	"github.com/vitelabs/go-vite/chain/utils"
	"github.com/vitelabs/go-vite/common/types"
	"github.com/vitelabs/go-vite/ledger"
)

func (sDB *StateDB) undo(batch *leveldb.Batch, latestSnapshotBlock *ledger.SnapshotBlock) (*chain_block.Location, error) {
	logFileIdList, err := sDB.undoLogger.LogFileIdList()
	if err != nil {
		return nil, err
	}

	//	var undoBlockHashList []*types.Hash
	undoKeyMap := make(map[string]struct{})
	var location *chain_block.Location
	toHash := latestSnapshotBlock.Hash

LOOP:
	for _, logFileId := range logFileIdList {
		buf, err := sDB.undoLogger.ReadFile(logFileId)

		if err != nil {
			return nil, err
		}
		currentPointer := len(buf)

		for currentPointer > 0 {
			size := binary.BigEndian.Uint32(buf[currentPointer-4 : currentPointer])
			currentPointer = currentPointer - 4

			nextPointer := currentPointer - int(size)
			undoLogBuffer := buf[nextPointer:currentPointer]

			if bytes.Equal(toHash.Bytes(), undoLogBuffer[:types.HashSize]) {

				location = chain_block.NewLocation(logFileId, int64(currentPointer))

				break LOOP
			}

			parseUndoLogBuffer(undoKeyMap, undoLogBuffer[types.HashSize:])
			currentPointer = nextPointer

		}
	}

	if len(undoKeyMap) > 0 {
		if err := sDB.undoKeys(batch, undoKeyMap, latestSnapshotBlock.Height); err != nil {
			return nil, err
		}
	}

	return location, nil
}

func (sDB *StateDB) undoKeys(batch *leveldb.Batch, undoKeys map[string]struct{}, snapshotHeight uint64) error {
	for undoKeyStr := range undoKeys {
		undoKey := []byte(undoKeyStr)

		undoKey[0] += 1

		iter := sDB.db.NewIterator(util.BytesPrefix(undoKey), nil)
		iterOk := iter.Last()

		for iterOk {
			key := iter.Key()
			height := binary.BigEndian.Uint64(key[len(key)-8:])
			if height > snapshotHeight {
				batch.Delete(key)
			} else {
				undoKey[0] -= 1
				batch.Put(undoKey, iter.Value())
			}

			iterOk = iter.Prev()
		}
		if err := iter.Error(); err != nil && err != leveldb.ErrNotFound {
			iter.Release()
			return err
		}

		iter.Release()
	}
	return nil
}

func parseUndoLogBuffer(undoKeys map[string]struct{}, undoLogBuffer []byte) {
	currentPointer := 0
	undoLogBufferLen := len(undoLogBuffer)
	if currentPointer < undoLogBufferLen {
		keyType := undoLogBuffer[currentPointer+1]

		var nextPointer = 0
		switch keyType {
		case chain_utils.StorageKeyPrefix:
			nextPointer = currentPointer + types.AddressSize + 34

		case chain_utils.BalanceKeyPrefix:
			nextPointer = currentPointer + 1 + types.AddressSize + types.TokenTypeIdSize
		}

		undoKeys[string(undoLogBuffer[currentPointer:nextPointer])] = struct{}{}
		currentPointer = nextPointer
	}
}