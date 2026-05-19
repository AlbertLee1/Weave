import { Navigate, useParams } from 'react-router';

export function VertexDiagrammingRedirect() {
  const { rid } = useParams<{ rid?: string }>();
  return <Navigate to={rid ? `/vertex/${rid}` : '/vertex/new'} replace />;
}
