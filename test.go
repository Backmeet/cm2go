package main

import (
	"fmt"
	"image/color"
	"math"
	"sort"
	"time"

	"github.com/faiface/pixel"
	"github.com/faiface/pixel/imdraw"
	"github.com/faiface/pixel/pixelgl"
	"github.com/faiface/pixel/text"
	"golang.org/x/image/colornames"
	"golang.org/x/image/font/basicfont"
	"gonum.org/v1/gonum/mat"
	"gonum.org/v1/gonum/spatial/r3"
)

type RenderItem struct {
	Points []r3.Vec
	Color  color.Color
}

type Renderer3D struct {
	Render3D             []RenderItem
	cPOS                 r3.Vec
	fov                  float64
	screenw, screenh     float64
	cachedRotationMatrix *mat.Dense
	renderDistance       float64
}

func tick(lastTick *time.Time, targetFPS int) float64 {
	now := time.Now()
	dt := now.Sub(*lastTick)
	if targetFPS > 0 {
		targetFrame := time.Second / time.Duration(targetFPS)
		if dt < targetFrame {
			time.Sleep(targetFrame - dt)
			now = time.Now()
			dt = now.Sub(*lastTick)
		}
	}
	*lastTick = now
	return dt.Seconds()
}

func multiplyMatVec(m *mat.Dense, v r3.Vec) r3.Vec {
	return r3.Vec{
		X: m.At(0, 0)*v.X + m.At(0, 1)*v.Y + m.At(0, 2)*v.Z,
		Y: m.At(1, 0)*v.X + m.At(1, 1)*v.Y + m.At(1, 2)*v.Z,
		Z: m.At(2, 0)*v.X + m.At(2, 1)*v.Y + m.At(2, 2)*v.Z,
	}
}

// Correct rotation matrix: yaw (Y axis), pitch (X axis), roll (Z axis)
func RotationMatrix(yawDeg, pitchDeg, rollDeg float64) *mat.Dense {
	yaw := yawDeg * math.Pi / 180
	pitch := pitchDeg * math.Pi / 180
	roll := rollDeg * math.Pi / 180

	cy, sy := math.Cos(yaw), math.Sin(yaw)
	cp, sp := math.Cos(pitch), math.Sin(pitch)
	cr, sr := math.Cos(roll), math.Sin(roll)

	data := []float64{
		cy*cr + sy*sp*sr, sr * cp, -sy*cr + cy*sp*sr,
		-cy*sr + sy*sp*cr, cr * cp, sr*sy + cy*sp*cr,
		sy * cp, -sp, cy * cp,
	}
	return mat.NewDense(3, 3, data)
}

func (r *Renderer3D) convert3DTo2D(point r3.Vec) (pixel.Vec, bool) {
	diff := r3.Vec{X: point.X - r.cPOS.X, Y: point.Y - r.cPOS.Y, Z: point.Z - r.cPOS.Z}
	pointCam := multiplyMatVec(r.cachedRotationMatrix, diff)
	if pointCam.Z <= 0 || math.Sqrt(pointCam.X*pointCam.X+pointCam.Y*pointCam.Y+pointCam.Z*pointCam.Z) > r.renderDistance {
		return pixel.ZV, false
	}
	x2d := r.fov*(pointCam.X/pointCam.Z) + r.screenw/2
	y2d := r.fov*(pointCam.Y/pointCam.Z) + r.screenh/2
	return pixel.V(x2d, y2d), true
}

func (r *Renderer3D) NewCube(colors []color.Color, pos, orientation r3.Vec, lx, ly, lz float64, fill bool) []RenderItem {
	if len(colors) < 6 {
		c := colors[0]
		colors = []color.Color{c, c, c, c, c, c}
	}
	rot := RotationMatrix(orientation.Y, orientation.X, orientation.Z)
	verts := []r3.Vec{
		{X: 0, Y: 0, Z: 0}, {X: lx, Y: 0, Z: 0}, {X: 0, Y: ly, Z: 0}, {X: 0, Y: 0, Z: lz},
		{X: lx, Y: ly, Z: 0}, {X: lx, Y: 0, Z: lz}, {X: 0, Y: ly, Z: lz}, {X: lx, Y: ly, Z: lz},
	}
	for i, v := range verts {
		rv := multiplyMatVec(rot, v)
		verts[i] = r3.Vec{X: rv.X + pos.X, Y: rv.Y + pos.Y, Z: rv.Z + pos.Z}
	}
	if fill {
		return []RenderItem{
			{Points: []r3.Vec{verts[0], verts[1], verts[4], verts[2]}, Color: colors[0]},
			{Points: []r3.Vec{verts[0], verts[1], verts[5], verts[3]}, Color: colors[1]},
			{Points: []r3.Vec{verts[0], verts[2], verts[6], verts[3]}, Color: colors[2]},
			{Points: []r3.Vec{verts[4], verts[7], verts[6], verts[2]}, Color: colors[3]},
			{Points: []r3.Vec{verts[5], verts[7], verts[6], verts[3]}, Color: colors[4]},
			{Points: []r3.Vec{verts[4], verts[7], verts[5], verts[1]}, Color: colors[5]},
		}
	}
	edges := []RenderItem{}
	idxs := [][2]int{{0, 1}, {0, 2}, {0, 3}, {1, 4}, {1, 5}, {2, 4}, {2, 6}, {3, 5}, {3, 6}, {4, 7}, {5, 7}, {6, 7}}
	for i, e := range idxs {
		col := colors[i/2]
		edges = append(edges, RenderItem{Points: []r3.Vec{verts[e[0]], verts[e[1]]}, Color: col})
	}
	return edges
}

func (r *Renderer3D) Tick(win pixel.Target) {
	imd := imdraw.New(nil)
	type obj struct {
		depth  float64
		points []pixel.Vec
		col    color.Color
	}
	objs := []obj{}
	for _, item := range r.Render3D {
		points2d := []pixel.Vec{}
		valid := true
		sumDepth := 0.0
		for _, pt := range item.Points {
			proj, ok := r.convert3DTo2D(pt)
			if !ok {
				valid = false
				break
			}
			points2d = append(points2d, proj)
			dx, dy, dz := pt.X-r.cPOS.X, pt.Y-r.cPOS.Y, pt.Z-r.cPOS.Z
			sumDepth += math.Sqrt(dx*dx + dy*dy + dz*dz)
		}
		if valid {
			objs = append(objs, obj{depth: -sumDepth / float64(len(item.Points)), points: points2d, col: item.Color})
		}
	}
	sort.Slice(objs, func(i, j int) bool { return objs[i].depth < objs[j].depth })
	for _, o := range objs {
		imd.Color = o.col
		switch len(o.points) {
		case 1:
			imd.Push(o.points[0])
			imd.Circle(1, 0)
		case 2:
			imd.Push(o.points[0], o.points[1])
			imd.Line(2)
		default:
			imd.Push(o.points...)
			imd.Polygon(0)
		}
	}
	imd.Draw(win)
}

func run() {
	cfg := pixelgl.WindowConfig{
		Title:  "Flat Builder 3D",
		Bounds: pixel.R(0, 0, 900, 600),
		VSync:  true,
	}
	win, _ := pixelgl.NewWindow(cfg)

	renderer := &Renderer3D{
		Render3D:             []RenderItem{},
		cPOS:                 r3.Vec{X: 0, Y: 20, Z: -50},
		fov:                  400,
		screenw:              900,
		screenh:              600,
		cachedRotationMatrix: mat.NewDense(3, 3, []float64{1, 0, 0, 0, 1, 0, 0, 0, 1}),
		renderDistance:       500,
	}

	gridSize := 5
	for x := -50; x <= 50; x += gridSize {
		for z := -50; z <= 50; z += gridSize {
			cube := renderer.NewCube([]color.Color{colornames.Gray}, r3.Vec{X: float64(x), Y: 0, Z: float64(z)}, r3.Vec{}, float64(gridSize), 1, float64(gridSize), true)
			renderer.Render3D = append(renderer.Render3D, cube...)
		}
	}

	blockColors := []color.Color{
		colornames.Red, colornames.Green, colornames.Blue, colornames.Yellow,
		colornames.Magenta, colornames.Cyan, colornames.Orange, colornames.Purple,
		colornames.Brown, colornames.White,
	}
	selected := 0
	placedBlocks := []r3.Vec{}

	atlas := text.NewAtlas(basicfont.Face7x13, text.ASCII)
	txt := text.New(pixel.V(10, 580), atlas)

	lastTick := time.Now()
	yaw, pitch, roll := 0.0, 0.0, 0.0
	var lastPos r3.Vec
	var lastYaw, lastPitch, lastRoll float64
	mouseGrabbed := true
	firstClickAfterFocus := false
	center := win.Bounds().Center()
	win.SetCursorVisible(false)

	for !win.Closed() {
		dt := tick(&lastTick, 60)
		win.Clear(colornames.Black)

		var mouseDelta pixel.Vec
		if mouseGrabbed && win.Focused() {
			mouseDelta = win.MousePosition().Sub(center)
			win.SetMousePosition(center)
		}

		if win.JustPressed(pixelgl.KeyEscape) && mouseGrabbed {
			mouseGrabbed = false
			win.SetCursorVisible(true)
			firstClickAfterFocus = true
		}
		if win.JustPressed(pixelgl.MouseButtonLeft) && !mouseGrabbed {
			mouseGrabbed = true
			win.SetCursorVisible(false)
			win.SetMousePosition(center)
			firstClickAfterFocus = false
		}

		if mouseGrabbed {
			yaw += mouseDelta.X * 0.2
			pitch -= mouseDelta.Y * 0.2
			if pitch > 85 {
				pitch = 85
			} else if pitch < -85 {
				pitch = -85
			}
		}

		if renderer.cPOS != lastPos || yaw != lastYaw || pitch != lastPitch || roll != lastRoll {
			renderer.cachedRotationMatrix = RotationMatrix(yaw, pitch, roll)
			lastPos = renderer.cPOS
			lastYaw, lastPitch, lastRoll = yaw, pitch, roll
		}

		rot := renderer.cachedRotationMatrix
		forward := r3.Vec{X: -rot.At(2, 0), Y: -rot.At(2, 1), Z: -rot.At(2, 2)}
		right := r3.Vec{X: rot.At(0, 0), Y: rot.At(0, 1), Z: rot.At(0, 2)}
		up := r3.Vec{X: rot.At(1, 0), Y: rot.At(1, 1), Z: rot.At(1, 2)}

		speed := 20.0 * dt
		if win.Pressed(pixelgl.KeyW) {
			renderer.cPOS = r3.Add(renderer.cPOS, r3.Scale(-speed, forward))
		}
		if win.Pressed(pixelgl.KeyS) {
			renderer.cPOS = r3.Add(renderer.cPOS, r3.Scale(speed, forward))
		}
		if win.Pressed(pixelgl.KeyD) {
			renderer.cPOS = r3.Add(renderer.cPOS, r3.Scale(speed, right))
		}
		if win.Pressed(pixelgl.KeyA) {
			renderer.cPOS = r3.Add(renderer.cPOS, r3.Scale(-speed, right))
		}
		if win.Pressed(pixelgl.KeySpace) {
			renderer.cPOS = r3.Add(renderer.cPOS, r3.Scale(speed, up))
		}
		if win.Pressed(pixelgl.KeyLeftControl) {
			renderer.cPOS = r3.Add(renderer.cPOS, r3.Scale(-speed, up))
		}

		if win.JustPressed(pixelgl.MouseButtonLeft) && mouseGrabbed && !firstClickAfterFocus {
			forwardNorm := r3.Scale(1/math.Sqrt(forward.X*forward.X+forward.Y*forward.Y+forward.Z*forward.Z), forward)
			step, maxDist := float64(gridSize)/2, 50.0 // larger step, faster
			var hit r3.Vec
			placed := false

			for d := 0.0; d < maxDist; d += step {
				p := r3.Add(renderer.cPOS, r3.Scale(d, forwardNorm))
				snap := r3.Vec{
					X: math.Floor(p.X/float64(gridSize)) * float64(gridSize),
					Y: math.Floor(p.Y/float64(gridSize)) * float64(gridSize),
					Z: math.Floor(p.Z/float64(gridSize)) * float64(gridSize),
				}

				// Baseplate: only if y == 0 exactly
				if snap.Y <= 0 || snap.Y >= 1 {
					hit = snap
					placed = true
					break
				}

				// Check if ray hits an existing block
				var hitBlock r3.Vec
				blockExists := false
				for _, b := range placedBlocks {
					if b == snap {
						blockExists = true
						hitBlock = b
						break
					}
				}

				if blockExists {
					// Determine which face we hit relative to block
					localPos := r3.Sub(p, hitBlock)
					offset := r3.Vec{}
					if math.Abs(localPos.X) > math.Abs(localPos.Y) && math.Abs(localPos.X) > math.Abs(localPos.Z) {
						if localPos.X > 0 {
							offset.X = float64(gridSize)
						} else {
							offset.X = -float64(gridSize)
						}
					} else if math.Abs(localPos.Y) > math.Abs(localPos.Z) {
						if localPos.Y > 0 {
							offset.Y = float64(gridSize)
						} else {
							offset.Y = -float64(gridSize)
						}
					} else {
						if localPos.Z > 0 {
							offset.Z = float64(gridSize)
						} else {
							offset.Z = -float64(gridSize)
						}
					}
					hit = r3.Add(hitBlock, offset)
					placed = true
					break
				}
			}

			// Prevent overlapping block
			if placed {
				overlap := false
				for _, b := range placedBlocks {
					if b == hit {
						overlap = true
						break
					}
				}
				if !overlap {
					placedBlocks = append(placedBlocks, hit)
					cube := renderer.NewCube([]color.Color{blockColors[selected]}, hit, r3.Vec{}, float64(gridSize), float64(gridSize), float64(gridSize), true)
					renderer.Render3D = append(renderer.Render3D, cube...)
				}
			}
		}

		renderer.Tick(win)

		imd := imdraw.New(nil)
		imd.Color = colornames.White
		imd.Push(win.Bounds().Center())
		imd.Circle(2, 0)
		imd.Draw(win)

		txt.Clear()
		fmt.Fprintf(txt, "Selected: %d\nPlaced: %d", selected, len(placedBlocks))
		txt.Draw(win, pixel.IM)

		win.Update()
	}
}

func main() {
	pixelgl.Run(run)
}
