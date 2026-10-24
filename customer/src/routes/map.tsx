import { createFileRoute } from '@tanstack/react-router';
import { MapPage } from '../map/MapPage';

export const Route = createFileRoute('/map')({
  component: MapPage,
});
