# Landing photos: sources

Photos on the bright landing (D-262). Product and shop names in them are the makers' trademarks; the photos only show goods a neighbourhood shop sells.

## Hero photos (Unsplash licence)

Free for commercial use, no credit required ([unsplash.com/license](https://unsplash.com/license)). They replace the trial's street and tea-stall photos (founder, 26 Oct: "the background image is not good"). Encoded from the full-resolution originals (founder, 27 Oct: the 800 px phone copy looked soft): landscape AVIF at 1200, 1920 and 2560 px (SVT-AV1, CRF 34), a portrait crop around the shopkeeper for phones (`-phone-750.avif`, `-phone-1170.avif`), and WebP at 1200 and 1920 px (q75) for browsers without AVIF. `components/bright/heroPhotos.ts` wires them into the hero's `<picture>`.

| File | Page | Photo | Photographer |
|---|---|---|---|
| `hero-kirana-*` | Home | https://unsplash.com/photos/a-man-stands-in-a-small-convenience-store-gakuij2BVdg | Rohit Dey |
| `hero-shopkeeper-*` | For retailers | https://unsplash.com/photos/a-man-smiles-in-a-packed-open-front-store-g3IR5Y3C730 (Kochi) | Wietse Jongsma |

## Product photos (CC BY 2.0, trial photos)

These came with the founder's trial (design/trials/site-home-bright) and are credited on the home page (`PhotoCredits.astro`, `PHOTO_CREDITS` in `components/bright/data.ts`). Replace them with our own product shots before launch.

| File | Title | Creator | Source |
|---|---|---|---|
| `p-milk.jpg` | Milk Bottle | NickPiggott | flickr.com/photos/93656595@N00/513371114 |
| `p-choc.jpg` | chocolate bar Ikea | molinolin | flickr.com/photos/27938098@N05/3612036798 |
| `shop-pharmacy.jpg` | Pharmacy, Gangtok, India | flowcomm | flickr.com/photos/21162417@N07/8083933798 |
| `p-cable.jpg` | USB Cable | wwarby | flickr.com/photos/26782864@N00/11728892063 |
| `p-bread.jpg` | Bread Loaf | Teuobk | flickr.com/photos/13261823@N05/2104039823 |
| `p-earphones.jpg` | DIY: Shure Sound Isolating Earphone + iPhone 4 Earphone | Yutaka Tsutano | flickr.com/photos/23208360@N07/4737693376 |
| `p-wash.jpg` | Fabric softener/conditioner at dm-drogeriemarkt in Berlin, Germany | qubodup | flickr.com/photos/21051491@N02/7980493529 |
| `p-noodles.jpg` | Instant noodles with added stuff | Annie Mole | flickr.com/photos/21309047@N00/2416809777 |

`todayz-mark-96.png` is the TodayZ mark.
