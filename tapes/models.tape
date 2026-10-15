# The models demo: a GIF to share, and the gallery's poster frame.
# Run from the repository root: task tapes.
Output dist/tapes/models.gif
Source tapes/common.tape
Hide
Type "bin/nt3d-models"
Enter
Wait+Screen@60s /fps/
Sleep 1s
Show
Type "r"
Sleep 3s
# Poster candidates: finish.sh keeps the first that shows the scene, as
# a screenshot can land while the Kitty image is being replaced.
Screenshot dist/tapes/models-1.png
Sleep 0.4s
Screenshot dist/tapes/models-2.png
Sleep 0.4s
Screenshot dist/tapes/models-3.png
Type "]"
Sleep 3s
Type "]"
Sleep 3s
