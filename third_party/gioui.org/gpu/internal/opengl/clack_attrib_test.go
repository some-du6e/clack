// SPDX-License-Identifier: Unlicense OR MIT

//go:build !js

package opengl

import (
	"testing"

	"gioui.org/internal/gl"
)

// fakeAttribs answers attribute queries from a table, as OpenGL would for
// the attributes another user of the context left set up.
type fakeAttribs map[gl.Enum]int

func (f fakeAttribs) GetVertexAttrib(_ int, pname gl.Enum) int { return f[pname] }
func (f fakeAttribs) GetVertexAttribBinding(_ int, pname gl.Enum) gl.Object {
	return gl.Object{V: uint(f[pname])}
}
func (f fakeAttribs) GetVertexAttribPointer(_ int, pname gl.Enum) uintptr {
	return uintptr(f[pname])
}

// Clack patch 37: an enabled attribute whose buffer was deleted (its
// binding 0) must not look like it points into buffer 1, the name the
// next glGenBuffers hands out; the first bind after a state query then
// calls OpenGL. Gio read the binding from VERTEX_ATTRIB_ARRAY_ENABLED.
func TestQueryVertAttribBinding(t *testing.T) {
	left := fakeAttribs{
		gl.VERTEX_ATTRIB_ARRAY_ENABLED:        1,
		gl.VERTEX_ATTRIB_ARRAY_BUFFER_BINDING: 0,
		gl.VERTEX_ATTRIB_ARRAY_SIZE:           2,
		gl.VERTEX_ATTRIB_ARRAY_TYPE:           int(gl.FLOAT),
		gl.VERTEX_ATTRIB_ARRAY_STRIDE:         16,
		gl.VERTEX_ATTRIB_ARRAY_POINTER:        8,
	}
	a := queryVertAttrib(left, 0)
	if !a.enabled || a.size != 2 || a.typ != gl.FLOAT || a.stride != 16 || a.offset != 8 {
		t.Fatalf("queried %+v, want enabled, 2 floats, stride 16, offset 8", a)
	}
	if !a.obj.Equal(gl.Buffer{}) {
		t.Errorf("an attribute bound to no buffer points into %v", a.obj)
	}
	quad := gl.Buffer{V: 1}
	if a.points(quad, 2, gl.FLOAT, false, 16, 8) {
		t.Errorf("after the query the bind into buffer 1 would be left out: the draw would read client memory")
	}
	// bound to a real buffer, the same bind is left out, another isn't
	left[gl.VERTEX_ATTRIB_ARRAY_BUFFER_BINDING] = 7
	a = queryVertAttrib(left, 0)
	if !a.points(gl.Buffer{V: 7}, 2, gl.FLOAT, false, 16, 8) {
		t.Errorf("an attribute pointing into buffer 7 as given isn't seen to")
	}
	if a.points(gl.Buffer{V: 7}, 2, gl.FLOAT, false, 16, 0) || a.points(quad, 2, gl.FLOAT, false, 16, 8) {
		t.Errorf("a different offset or buffer counts as the same")
	}
}
