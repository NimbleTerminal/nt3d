# The dynamic demo: a GIF to share, and the gallery's poster frame.
# Run from the repository root: task tapes.
Output dist/tapes/dynamic.gif
Source tapes/common.tape
Hide
Type "bin/nt3d-dynamic"
Enter
Wait+Screen@60s /fps/
Sleep 1s
Show
Type "r"
Sleep 3s
# Poster candidates: finish.sh keeps the first that shows the scene, as
# a screenshot can land while the Kitty image is being replaced.
Screenshot dist/tapes/dynamic-1.png
Sleep 0.4s
Screenshot dist/tapes/dynamic-2.png
Sleep 0.4s
Screenshot dist/tapes/dynamic-3.png
Type "b"
Sleep 1s
Type "b"
Sleep 1s
Type "b"
Sleep 2s
Type "m"
Sleep 3s
