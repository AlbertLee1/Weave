// US-428: shared widget type definitions extracted from DashboardEditorPage.
// The widget union now carries an optional dataSource binding so chart / stat
// / map widgets can pull values from a live aggregation instead of the
// inline-configured array.

export type ChartType = 'bar' | 'line' | 'pie';
export type StatTrend = 'up' | 'down' | 'neutral';

// DataSource binds a widget to either an inline literal (the existing
// behaviour, preserves byte-for-byte compatibility for legacy dashboards)
// or a live aggregation request against an ontology object type.
//
// We kept this narrow on purpose — the AC asks for "ObjectSet / Aggregation"
// data binding; the canonical wire surface is the aggregation endpoint
// (US-016/US-026), so a single 'aggregation' kind covers both.
export interface DataSourceInline {
  kind: 'inline';
}
export interface DataSourceAggregation {
  kind: 'aggregation';
  ontology: string;
  objectType: string;
  metric: 'count' | 'sum' | 'avg' | 'min' | 'max';
  // For sum/avg/min/max the property is required; for count it's ignored.
  property?: string;
  // Optional groupBy — when set, each bucket becomes a chart bar / pie slice.
  groupBy?: string;
}
export type WidgetDataSource = DataSourceInline | DataSourceAggregation;

export interface BaseWidget {
  id: string;
  title: string;
  x: number;
  y: number;
  w: number;
  h: number;
}

export interface TextWidget extends BaseWidget {
  type: 'text';
  content: string;
}

export interface ChartWidget extends BaseWidget {
  type: 'chart';
  chartType: ChartType;
  values: number[];
  // Optional bucket labels paired with `values` (used by line/bar x-axis +
  // pie slice legend). Falls back to numeric indices when absent.
  labels?: string[];
  dataSource?: WidgetDataSource;
}

export interface TableWidget extends BaseWidget {
  type: 'table';
  columns: string[];
  rows: string[][];
}

export interface StatWidget extends BaseWidget {
  type: 'stat';
  value: string;
  label: string;
  trend: StatTrend;
  // Optional sparkline series — when present (or when the dataSource fills
  // it), the stat card renders a small SVG line under the number.
  sparkline?: number[];
  dataSource?: WidgetDataSource;
}

export interface MapWidget extends BaseWidget {
  type: 'map';
  latitude: number;
  longitude: number;
  zoom: number;
  // Optional GeoJSON overlay (Feature / FeatureCollection / bare Geometry).
  // Persisted as a structured object so the editor can store it as JSON
  // text in the config panel and round-trip cleanly.
  geojson?: unknown;
}

export type Widget =
  | TextWidget
  | ChartWidget
  | TableWidget
  | StatWidget
  | MapWidget;
export type WidgetType = Widget['type'];
