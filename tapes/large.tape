# The large demo: a GIF to share, and the gallery's poster frame.
# Run from the repository root: task tapes.
Output dist/tapes/large.gif
Source tapes/common.tape
Hide
Type "bin/nt3d-large"
Enter
Wait+Screen@60s /fps/
Sleep 1s
Show
Type "r"
Sleep 3s
# Poster candidates: finish.sh keeps the first that shows the scene, as
# a screenshot can land while the Kitty image is being replaced.
Screenshot dist/tapes/large-1.png
Sleep 0.4s
Screenshot dist/tapes/large-2.png
Sleep 0.4s
Screenshot dist/tapes/large-3.png
Type "2"
Sleep 3s
Type "1"
Sleep 2s
