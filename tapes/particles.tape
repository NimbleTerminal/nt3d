# The particles demo: a GIF to share, and the gallery's poster frame.
# Run from the repository root: task tapes.
Output dist/tapes/particles.gif
Source tapes/common.tape
Hide
Type "bin/nt3d-particles"
Enter
Wait+Screen@60s /fps/
# The poster shows the default: fire additive, smoke alpha, 10,000 points.
Sleep 2s
Show
Sleep 3s
# Poster candidates: finish.sh keeps the first that shows the scene, as
# a screenshot can land while the Kitty image is being replaced.
Screenshot dist/tapes/particles-1.png
Sleep 0.4s
Screenshot dist/tapes/particles-2.png
Sleep 0.4s
Screenshot dist/tapes/particles-3.png
Sleep 2s
# Then 100,000 points, all alpha, and hard discs.
Type "n"
Sleep 3s
Type "b"
Sleep 3s
Type "s"
Sleep 3s
