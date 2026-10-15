# The procedural demo: a GIF to share, and the gallery's poster frame.
# Run from the repository root: task tapes.
Output dist/tapes/procedural.gif
Source tapes/common.tape
Hide
Type "bin/nt3d-procedural"
Enter
Wait+Screen@60s /fps/
Type "+++"
Sleep 1s
Show
Sleep 5s
# Poster candidates: finish.sh keeps the first that shows the scene, as
# a screenshot can land while the Kitty image is being replaced.
Screenshot dist/tapes/procedural-1.png
Sleep 0.4s
Screenshot dist/tapes/procedural-2.png
Sleep 0.4s
Screenshot dist/tapes/procedural-3.png
Type "t"
Sleep 4s
