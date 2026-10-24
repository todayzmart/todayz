import type { StoreItem } from '@todayz/api-client/gen/model/storeItem';
import { itemTitle } from '../stock/format';
import type { PickedItem } from './form';

export const toPicked = (i: StoreItem): PickedItem => ({ id: i.id, title: itemTitle(i), pricePaise: i.pricePaise });
