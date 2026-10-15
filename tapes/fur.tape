# The fur demo: a GIF to share, and the gallery's poster frame.
# Run from the repository root: task tapes.
Output dist/tapes/fur.gif
Source tapes/common.tape
Hide
Type "bin/nt3d-fur"
Enter
Wait+Screen@60s /fps/
Sleep 1s
Show
Type "r"
Sleep 3s
# Poster candidates: finish.sh keeps the first that shows the scene, as
# a screenshot can land while the Kitty image is being replaced.
Screenshot dist/tapes/fur-1.png
Sleep 0.4s
Screenshot dist/tapes/fur-2.png
Sleep 0.4s
Screenshot dist/tapes/fur-3.png
Type "s"
Sleep 3s
Type "l"
Sleep 3s
