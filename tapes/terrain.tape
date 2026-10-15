# The terrain demo: a GIF to share, and the gallery's poster frame.
# Run from the repository root: task tapes.
Output dist/tapes/terrain.gif
Source tapes/common.tape
Hide
Type "bin/nt3d-terrain"
Enter
Wait+Screen@60s /fps/
Type "+++"
Sleep 1s
Show
Type "r"
Sleep 3s
# Poster candidates: finish.sh keeps the first that shows the scene, as
# a screenshot can land while the Kitty image is being replaced.
Screenshot dist/tapes/terrain-1.png
Sleep 0.4s
Screenshot dist/tapes/terrain-2.png
Sleep 0.4s
Screenshot dist/tapes/terrain-3.png
Type "t"
Sleep 2.5s
Type "3"
Sleep 2.5s
Type "1"
Sleep 2s
