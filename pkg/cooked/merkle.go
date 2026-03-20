package cooked

import (
	"bytes"
	"crypto/sha256"
	"fmt"
)

// MerkleCheckpoint represents a snapshot of the hash chain organized as a Merkle tree.
type MerkleCheckpoint struct {
	CheckpointID    [16]byte `json:"checkpoint_id" db:"checkpoint_id"`
	RootHash        []byte   `json:"root_hash" db:"root_hash"`
	FirstRowID      [16]byte `json:"first_row_id" db:"first_row_id"`
	LastRowID       [16]byte `json:"last_row_id" db:"last_row_id"`
	RowCount        int64    `json:"row_count" db:"row_count"`
	PrevCheckpointID *[16]byte `json:"prev_checkpoint_id,omitempty" db:"prev_checkpoint_id"`
}

// MerkleProof is an inclusion proof for a single row within a checkpoint.
// It can be verified without database access — hand it to an auditor.
type MerkleProof struct {
	RowHash      []byte      `json:"row_hash"`
	RootHash     []byte      `json:"root_hash"`
	CheckpointID [16]byte    `json:"checkpoint_id"`
	Siblings     []ProofNode `json:"siblings"`
}

// ProofNode is a sibling hash in a Merkle inclusion proof.
type ProofNode struct {
	Hash []byte `json:"hash"`
	Left bool   `json:"left"` // true if this sibling is on the left
}

// VerifyProof checks a Merkle inclusion proof. This is a pure function —
// it needs no database access. It can run on a client, auditor, or third party.
func VerifyProof(proof *MerkleProof) bool {
	if proof == nil || len(proof.RowHash) == 0 || len(proof.RootHash) == 0 {
		return false
	}

	current := make([]byte, len(proof.RowHash))
	copy(current, proof.RowHash)

	for _, sibling := range proof.Siblings {
		h := sha256.New()
		if sibling.Left {
			h.Write(sibling.Hash)
			h.Write(current)
		} else {
			h.Write(current)
			h.Write(sibling.Hash)
		}
		current = h.Sum(nil)
	}

	return bytes.Equal(current, proof.RootHash)
}

// buildMerkleTree builds a Merkle tree from leaf hashes and returns the root hash.
// Non-power-of-2 leaf counts are handled by promoting the last leaf (not duplicating).
// Returns the root hash and the full tree as a flat array (level by level).
func buildMerkleTree(leaves [][]byte) (root []byte, tree [][]byte) {
	if len(leaves) == 0 {
		return GenesisHash, nil
	}
	if len(leaves) == 1 {
		return leaves[0], leaves
	}

	// Copy leaves as the first level
	level := make([][]byte, len(leaves))
	copy(level, leaves)
	tree = make([][]byte, len(leaves))
	copy(tree, leaves)

	for len(level) > 1 {
		var nextLevel [][]byte
		for i := 0; i < len(level); i += 2 {
			if i+1 < len(level) {
				// Hash pair
				h := sha256.New()
				h.Write(level[i])
				h.Write(level[i+1])
				parent := h.Sum(nil)
				nextLevel = append(nextLevel, parent)
				tree = append(tree, parent)
			} else {
				// Odd leaf — promote (don't duplicate)
				nextLevel = append(nextLevel, level[i])
			}
		}
		level = nextLevel
	}

	return level[0], tree
}

// generateProof generates a Merkle inclusion proof for the leaf at the given index.
func generateProof(leaves [][]byte, leafIndex int) ([]ProofNode, error) {
	if leafIndex < 0 || leafIndex >= len(leaves) {
		return nil, fmt.Errorf("leaf index %d out of range [0, %d)", leafIndex, len(leaves))
	}
	if len(leaves) == 1 {
		return nil, nil // single leaf = root, no proof needed
	}

	var proof []ProofNode
	level := make([][]byte, len(leaves))
	copy(level, leaves)
	idx := leafIndex

	for len(level) > 1 {
		var nextLevel [][]byte
		nextIdx := idx / 2

		for i := 0; i < len(level); i += 2 {
			if i+1 < len(level) {
				h := sha256.New()
				h.Write(level[i])
				h.Write(level[i+1])
				parent := h.Sum(nil)
				nextLevel = append(nextLevel, parent)

				// If this pair contains our index, record the sibling
				if i == idx || i+1 == idx {
					if i == idx {
						// Our node is on the left, sibling is on the right
						proof = append(proof, ProofNode{Hash: level[i+1], Left: false})
					} else {
						// Our node is on the right, sibling is on the left
						proof = append(proof, ProofNode{Hash: level[i], Left: true})
					}
				}
			} else {
				// Odd leaf — promoted
				nextLevel = append(nextLevel, level[i])
				// If this is our index, no sibling to record (promoted directly)
			}
		}
		level = nextLevel
		idx = nextIdx
	}

	return proof, nil
}
