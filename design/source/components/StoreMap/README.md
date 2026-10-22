# StoreMap

The first thing on the customer home: every shop around you on one map, how many there are, how many are open, how many products they hold, and the radius you are searching.

- Map: green pins for open shops, grey for closed, a white ring with a number for a cluster, the customer as a blue dot with a halo, and the search radius as a dashed green ring with a 6% green fill.
- "Live near you" sits top left; the legend top right; the expand button bottom right opens the full-screen map with a shop list sheet.
- Counts under the map are tabular numbers in three hairline cells and change with the radius.
- Radius chips: 1, 2, 5, 10 and 20 km. The selected chip is black like every selected chip. Default 2 km; the choice is remembered.
- The apps draw the map with MapLibre GL and a TodayZ style built from the `map-*` tokens (land, park, water, road, road edge), over OpenStreetMap data. No Google-style colours.
- Tapping a pin opens that shop's card in a bottom sheet; tapping a cluster zooms in.
- The marketing site and the desktop storefront list use the same card at a wider size with the counts to the left of the map.
