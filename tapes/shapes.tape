# The shapes demo: a GIF to share, and the gallery's poster frame.
# Run from the repository root: task tapes.
Output dist/tapes/shapes.gif
Source tapes/common.tape
Hide
Type "bin/nt3d-shapes"
Enter
Wait+Screen@60s /fps/
Sleep 1s
Show
Type "r"
Sleep 3s
# Poster candidates: finish.sh keeps the first that shows the scene, as
# a screenshot can land while the Kitty image is being replaced.
Screenshot dist/tapes/shapes-1.png
Sleep 0.4s
Screenshot dist/tapes/shapes-2.png
Sleep 0.4s
Screenshot dist/tapes/shapes-3.png
Type "s"
Sleep 2.5s
Type "t"
Sleep 3s
Type "d"
Sleep 2s
