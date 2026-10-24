# Shared waves, crest launches, and procedural water materials.
Output dist/tapes/water.gif
Output dist/tapes/water.txt
Source tapes/common.tape
Hide
Type "bin/nt3d-water --medium direct"
Enter
Wait+Screen@60s /fps/
Show
Wait+Screen@30s /airborne/
Type "a"
Wait+Screen@10s /paused/
Sleep 0.8s
Screenshot dist/tapes/water-1.png
Sleep 0.4s
Screenshot dist/tapes/water-2.png
Sleep 0.4s
Screenshot dist/tapes/water-3.png
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
