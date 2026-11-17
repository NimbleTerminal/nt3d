package nt3d

import "testing"

// placement stands for what an application knows about a node it placed.
type placement struct {
	kit  string
	slot int
}

func TestNodeDataTravelsWithTheNode(t *testing.T) {
	sphere, err := BuildSphere(Sphere{Radius: .8, Segments: 16, Rings: 8})
	check(t, err)
	s := NewScene()
	id, err := s.SetGeometry("sphere", sphere)
	check(t, err)

	if data, err := s.NodeData(id); err != nil || data != nil {
		t.Fatalf("a new node's data is %v, %v; want nil", data, err)
	}
	want := placement{kit: "harbor", slot: 3}
	check(t, s.SetNodeData(id, want))

	if data, err := s.NodeData(id); err != nil || data != want {
		t.Fatalf("NodeData = %v, %v; want %v", data, err, want)
	}
	if n, ok := s.Node(id); !ok || n.Data != want {
		t.Fatalf("Node.Data = %v, want %v", n.Data, want)
	}
	camera := DefaultCamera()
	f, err := s.Frame(camera, 320, 240)
	check(t, err)
	if len(f.Instances) != 1 || f.Instances[0].Data != want {
		t.Fatalf("frame instances %+v, want one with data %v", f.Instances, want)
	}
	if hit := s.Pick(camera, f, 160, 120, 0); hit == nil || hit.Data != want {
		t.Fatalf("picked %+v, want a hit with data %v", hit, want)
	}

	check(t, s.SetNodeData(id, nil))
	if data, _ := s.NodeData(id); data != nil {
		t.Fatalf("cleared data is %v", data)
	}
}

// A frame is a snapshot: data set afterwards belongs to later frames.
func TestNodeDataIsSnapshotByFrames(t *testing.T) {
	s, ids := drawOrderScene(t, "a")
	check(t, s.SetNodeData(ids["a"], "first"))
	f, err := s.Frame(DefaultCamera(), 32, 32)
	check(t, err)
	rev := s.Revision()
	check(t, s.SetNodeData(ids["a"], "second"))
	if s.Revision() == rev {
		t.Fatal("new node data left the scene's revision unchanged")
	}
	if f.Instances[0].Data != "first" {
		t.Fatalf("an earlier frame's data became %v", f.Instances[0].Data)
	}
}

func TestNodeDataRefusesUnknownNodes(t *testing.T) {
	s := NewScene()
	if err := s.SetNodeData(NodeID(1<<40), 1); err == nil {
		t.Fatal("SetNodeData accepted a node the scene does not have")
	}
	if _, err := s.NodeData(NodeID(1 << 40)); err == nil {
		t.Fatal("NodeData accepted a node the scene does not have")
	}
}
