import { MapContainer, TileLayer, Marker, GeoJSON } from 'react-leaflet';
import 'leaflet/dist/leaflet.css';

// US-428: split into its own module so the parent `MapView` can lazy-load
// it. Importing react-leaflet at module load reaches into the leaflet
// runtime which expects browser globals — the parent gates the import on
// an isBrowser() check.

interface Props {
  latitude: number;
  longitude: number;
  zoom: number;
  geojson: unknown;
}

export default function MapViewLeaflet({
  latitude,
  longitude,
  zoom,
  geojson,
}: Props) {
  return (
    <MapContainer
      data-testid="dashboard-widget-map-leaflet"
      center={[latitude, longitude]}
      zoom={zoom}
      scrollWheelZoom={false}
      className="absolute inset-0"
      style={{ width: '100%', height: '100%' }}
    >
      <TileLayer
        attribution='&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors'
        url="https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png"
      />
      <Marker position={[latitude, longitude]} />
      {geojson != null && (
        <GeoJSON
          data={geojson as Parameters<typeof GeoJSON>[0]['data']}
          style={() => ({
            color: '#f59e0b',
            fillColor: '#f59e0b',
            fillOpacity: 0.25,
            weight: 2,
          })}
        />
      )}
    </MapContainer>
  );
}
