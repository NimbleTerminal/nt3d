# Shoreline foam, flowing normals, tiled water, and scene optics.
Output dist/tapes/water.gif
Output dist/tapes/water.txt
Source tapes/common.tape
Hide
Type "bin/nt3d-water --medium direct"
Enter
Wait+Screen@60s /fps/
Show
Sleep 4s
Type "a"
Wait+Screen@10s /paused/
Sleep 0.8s
Screenshot dist/tapes/water-1.png
Sleep 0.4s
Screenshot dist/tapes/water-2.png
Sleep 0.4s
Screenshot dist/tapes/water-3.png
# Shore foam and flow comparisons at the same wave time.
Type "v"
Sleep 1s
Screenshot dist/tapes/water-no-flow.png
Type "v"
Sleep 1s
Screenshot dist/tapes/water-no-shore.png
Type "vv"
Type "m"
Sleep 2s
Screenshot dist/tapes/water-large.png
Type "3"
Sleep 1s
Screenshot dist/tapes/water-large-wire.png
Type "1m"
Sleep 1s
Type "f"
Sleep 1s
Screenshot dist/tapes/water-wake.png
Type "f"
Sleep 1s
Screenshot dist/tapes/water-spray.png
Type "f"
Sleep 1s
Screenshot dist/tapes/water-no-effects.png
Type "f"
# Compare scene transmission and particle intersections at a fixed instant.
Type "t"
Sleep 1s
Screenshot dist/tapes/water-no-optics.png
Type "t"
Type "h"
Sleep 1s
Screenshot dist/tapes/water-hard-spray.png
Type "h"
# Compare identical geometry with and without the two material contributions.
Type "n"
Sleep 1s
Screenshot dist/tapes/water-no-ripples.png
Type "e"
Sleep 1s
Screenshot dist/tapes/water-no-reflections.png
Type "ne"
Sleep 1s
Type "a"
Wait+Screen@10s /running/
Type "ww"
Sleep 3s
Type "j"
Wait+Screen@10s /manual/
Sleep 1s
Type " "
Wait+Screen@10s /autopilot/
Type "3"
Sleep 1s
Screenshot dist/tapes/water-wire.png
