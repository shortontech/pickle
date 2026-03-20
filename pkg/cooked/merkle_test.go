package cooked

import (
	"bytes"
	"crypto/sha256"
	"testing"
)

func TestBuildMerkleTreeEmpty(t *testing.T) {
	root, tree := buildMerkleTree(nil)
	if !bytes.Equal(root, GenesisHash) {
		t.Error("empty tree should return genesis hash")
	}
	if tree != nil {
		t.Error("empty tree should return nil nodes")
	}
}

func TestBuildMerkleTreeSingleLeaf(t *testing.T) {
	leaf := sha256Hash([]byte("hello"))
	root, _ := buildMerkleTree([][]byte{leaf})
	if !bytes.Equal(root, leaf) {
		t.Error("single-leaf tree root should equal the leaf")
	}
}

func TestBuildMerkleTreeTwoLeaves(t *testing.T) {
	a := sha256Hash([]byte("a"))
	b := sha256Hash([]byte("b"))
	root, _ := buildMerkleTree([][]byte{a, b})

	// Expected root: SHA-256(a || b)
	h := sha256.New()
	h.Write(a)
	h.Write(b)
	expected := h.Sum(nil)

	if !bytes.Equal(root, expected) {
		t.Errorf("two-leaf root mismatch:\n  got:    %x\n  expect: %x", root, expected)
	}
}

func TestBuildMerkleTreeFourLeaves(t *testing.T) {
	leaves := make([][]byte, 4)
	for i := range leaves {
		leaves[i] = sha256Hash([]byte{byte(i)})
	}
	root, _ := buildMerkleTree(leaves)
	if len(root) != 32 {
		t.Fatalf("root should be 32 bytes, got %d", len(root))
	}

	// Manually compute expected root
	h01 := sha256Pair(leaves[0], leaves[1])
	h23 := sha256Pair(leaves[2], leaves[3])
	expected := sha256Pair(h01, h23)

	if !bytes.Equal(root, expected) {
		t.Errorf("four-leaf root mismatch:\n  got:    %x\n  expect: %x", root, expected)
	}
}

func TestBuildMerkleTreeThreeLeaves(t *testing.T) {
	leaves := make([][]byte, 3)
	for i := range leaves {
		leaves[i] = sha256Hash([]byte{byte(i)})
	}
	root, _ := buildMerkleTree(leaves)

	// With 3 leaves: h01 = hash(l0, l1), then l2 is promoted
	// root = hash(h01, l2)
	h01 := sha256Pair(leaves[0], leaves[1])
	expected := sha256Pair(h01, leaves[2])

	if !bytes.Equal(root, expected) {
		t.Errorf("three-leaf root mismatch:\n  got:    %x\n  expect: %x", root, expected)
	}
}

func TestVerifyProofValid(t *testing.T) {
	leaves := make([][]byte, 4)
	for i := range leaves {
		leaves[i] = sha256Hash([]byte{byte(i)})
	}
	root, _ := buildMerkleTree(leaves)

	// Generate proof for leaf 0
	siblings, err := generateProof(leaves, 0)
	if err != nil {
		t.Fatal(err)
	}

	proof := &MerkleProof{
		RowHash:  leaves[0],
		RootHash: root,
		Siblings: siblings,
	}

	if !VerifyProof(proof) {
		t.Error("valid proof should verify")
	}
}

func TestVerifyProofAllLeaves(t *testing.T) {
	leaves := make([][]byte, 7) // non-power-of-2
	for i := range leaves {
		leaves[i] = sha256Hash([]byte{byte(i)})
	}
	root, _ := buildMerkleTree(leaves)

	for i := range leaves {
		siblings, err := generateProof(leaves, i)
		if err != nil {
			t.Fatalf("leaf %d: %v", i, err)
		}
		proof := &MerkleProof{
			RowHash:  leaves[i],
			RootHash: root,
			Siblings: siblings,
		}
		if !VerifyProof(proof) {
			t.Errorf("valid proof for leaf %d should verify", i)
		}
	}
}

func TestVerifyProofTampered(t *testing.T) {
	leaves := make([][]byte, 4)
	for i := range leaves {
		leaves[i] = sha256Hash([]byte{byte(i)})
	}
	root, _ := buildMerkleTree(leaves)

	siblings, err := generateProof(leaves, 0)
	if err != nil {
		t.Fatal(err)
	}

	// Tamper with the row hash
	tampered := make([]byte, 32)
	copy(tampered, leaves[0])
	tampered[0] ^= 0xFF

	proof := &MerkleProof{
		RowHash:  tampered,
		RootHash: root,
		Siblings: siblings,
	}

	if VerifyProof(proof) {
		t.Error("tampered proof should not verify")
	}
}

func TestVerifyProofNil(t *testing.T) {
	if VerifyProof(nil) {
		t.Error("nil proof should not verify")
	}
}

func TestGenerateProofOutOfRange(t *testing.T) {
	leaves := [][]byte{sha256Hash([]byte("a"))}
	_, err := generateProof(leaves, 1)
	if err == nil {
		t.Error("expected error for out-of-range index")
	}
}

func TestGenerateProofSingleLeaf(t *testing.T) {
	leaf := sha256Hash([]byte("a"))
	siblings, err := generateProof([][]byte{leaf}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(siblings) != 0 {
		t.Error("single-leaf proof should have no siblings")
	}
}

func sha256Hash(data []byte) []byte {
	h := sha256.Sum256(data)
	return h[:]
}

func sha256Pair(a, b []byte) []byte {
	h := sha256.New()
	h.Write(a)
	h.Write(b)
	return h.Sum(nil)
}
