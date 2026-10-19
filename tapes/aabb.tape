# The AABB tree demo: a GIF to share, and the gallery's poster frame.
# Run from the repository root: task tapes.
Output dist/tapes/aabb.gif
Source tapes/common.tape
Hide
Type "bin/nt3d-aabb"
Enter
Wait+Screen@60s /fps/
Sleep 1s
Show
# The poster shows the starting pick: a white box inside its ancestors.
Sleep 3s
# Poster candidates: finish.sh keeps the first that shows the scene, as
# a screenshot can land while the Kitty image is being replaced.
Screenshot dist/tapes/aabb-1.png
Sleep 0.4s
Screenshot dist/tapes/aabb-2.png
Sleep 0.4s
Screenshot dist/tapes/aabb-3.png
# Drift, so the ancestors stretch and the path reshapes.
Type "a"
Sleep 5s
# The whole tree, then the path again, turning.
Type "h"
Sleep 3s
Type "h"
Type "h"
Type "r"
Sleep 4s
