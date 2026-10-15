# The forest demo: a GIF to share, and the gallery's poster frame.
# Run from the repository root: task tapes.
Output dist/tapes/forest.gif
Source tapes/common.tape
Hide
Type "bin/nt3d-forest"
Enter
Wait+Screen@60s /fps/
Sleep 1s
Show
Sleep 3s
# Poster candidates: finish.sh keeps the first that shows the scene, as
# a screenshot can land while the Kitty image is being replaced.
Screenshot dist/tapes/forest-1.png
Sleep 0.4s
Screenshot dist/tapes/forest-2.png
Sleep 0.4s
Screenshot dist/tapes/forest-3.png
Sleep 5s
