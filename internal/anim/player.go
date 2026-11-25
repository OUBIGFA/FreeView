package anim

import "time"

// Player keeps an animation's playback position. It counts time in
// microseconds and keeps the remainder past each frame, so short delays
// do not drift: truncating to whole milliseconds on each frame slowly
// turns into slow motion. It is not safe for concurrent use.
type Player struct {
	a     *Animation
	frame int
	at    time.Duration // time spent in the current frame
	plays int           // plays finished
	done  bool
}

func NewPlayer(a *Animation) *Player { return &Player{a: a} }

// Frame is the index of the frame to show.
func (p *Player) Frame() int { return p.frame }

// Delay is how long the current frame shows in all.
func (p *Player) Delay() time.Duration { return p.a.Frames[p.frame].Delay }

// Remaining is how long, at speed, until the next frame is due.
func (p *Player) Remaining(speed float64) time.Duration {
	left := p.Delay() - p.at
	if speed > 0 {
		left = time.Duration(float64(left) / speed)
	}
	return max(left, time.Millisecond)
}

// Advance moves the clock by elapsed scaled by speed (1 is normal) and
// reports whether the frame changed. A late tick moves several frames,
// keeping what remains of the time. Playback stops on the last frame once
// the loop count is used up.
func (p *Player) Advance(elapsed time.Duration, speed float64) bool {
	if p.done || len(p.a.Frames) == 0 {
		return false
	}
	p.at += time.Duration(float64(elapsed.Microseconds())*speed) * time.Microsecond
	start := p.frame
	for p.at >= p.Delay() {
		p.at -= p.Delay()
		if p.frame+1 < len(p.a.Frames) {
			p.frame++
			continue
		}
		p.plays++
		if p.a.Loops > 0 && p.plays >= p.a.Loops {
			p.done, p.at = true, 0
			break
		}
		p.frame = 0
	}
	return p.frame != start
}

// Step moves delta frames, wrapping around, and starts the frame's time
// again.
func (p *Player) Step(delta int) {
	n := len(p.a.Frames)
	if n == 0 {
		return
	}
	p.frame = ((p.frame+delta)%n + n) % n
	p.at, p.done = 0, false
}

// Reset goes back to the first frame and restarts the loop count.
func (p *Player) Reset() { p.frame, p.at, p.plays, p.done = 0, 0, 0, false }

// Done reports whether the loop count is used up.
func (p *Player) Done() bool { return p.done }
